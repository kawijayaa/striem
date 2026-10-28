package database

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"math/big"
	"math/bits"
	"sort"
	"strconv"
	"time"
)

// Percentile sketches are merging t-digests. Up to exactDigestValues values
// are kept as-is, so nearest-rank percentiles of small groups are exact;
// beyond that values merge into at most a few hundred centroids, bounding
// memory as Kusto's T-Digest does.
const (
	exactDigestValues  = 4096
	digestCompression  = 200.0
	maxDigestEncoding  = 1 << 20
	maxHLLSparseHashes = 4096
)

type centroid struct{ mean, weight float64 }

type digest struct {
	kind      string
	exact     []float64 // exact values (with weights), before compression
	weights   []float64
	centroids []centroid
	count     float64
	min, max  float64
}

// digestValue maps a value of the given kind to a number: datetimes become
// ticks, timespans are ticks already, and Booleans are 0/1.
func digestValue(value any, kind string) (float64, bool) {
	if kind == "datetime" {
		when, ok := parsedDatetime(value)
		if !ok {
			return 0, false
		}
		ticks, _ := new(big.Float).SetInt(datetimeTicks(when)).Float64()
		return ticks, true
	}
	return number(value)
}

// digestResult converts a number back to the digest's kind.
func digestResult(value float64, kind string) any {
	switch kind {
	case "datetime":
		ticks, _ := new(big.Float).SetFloat64(math.Round(value)).Int(nil)
		return ticksDatetime(ticks)
	case "long", "int", "timespan":
		if math.Abs(value) >= 9.2e18 {
			return nil
		}
		return int64(math.Round(value))
	case "bool":
		return math.Round(value) != 0
	}
	return value
}

func (d *digest) add(value, weight float64) error {
	if weight <= 0 || math.IsNaN(weight) || math.IsInf(weight, 0) {
		return nil
	}
	if d.count == 0 || value < d.min {
		d.min = value
	}
	if d.count == 0 || value > d.max {
		d.max = value
	}
	d.count += weight
	if math.IsInf(d.count, 0) {
		return fmt.Errorf("percentile weights overflow")
	}
	d.exact = append(d.exact, value)
	d.weights = append(d.weights, weight)
	if len(d.exact) > exactDigestValues {
		d.compress()
	}
	return nil
}

// compress merges exact values and centroids into centroids sized by the
// k1 scale function, so the tails stay precise.
func (d *digest) compress() {
	points := make([]centroid, 0, len(d.centroids)+len(d.exact))
	points = append(points, d.centroids...)
	for i, value := range d.exact {
		points = append(points, centroid{value, d.weights[i]})
	}
	d.exact, d.weights = nil, nil
	sort.Slice(points, func(i, j int) bool { return points[i].mean < points[j].mean })
	var merged []centroid
	cumulative := 0.0
	for _, point := range points {
		if n := len(merged); n > 0 {
			last := &merged[n-1]
			q := (cumulative - last.weight + (last.weight+point.weight)/2) / d.count
			limit := 4 * d.count * q * (1 - q) / digestCompression
			if last.weight+point.weight <= math.Max(limit, 1) {
				last.mean += (point.mean - last.mean) * point.weight / (last.weight + point.weight)
				last.weight += point.weight
				cumulative += point.weight
				continue
			}
		}
		merged = append(merged, point)
		cumulative += point.weight
	}
	d.centroids = merged
}

func (d *digest) merge(other *digest) error {
	if other == nil || other.count == 0 {
		return nil
	}
	if d.kind == "" {
		d.kind = other.kind
	}
	for i, value := range other.exact {
		if err := d.add(value, other.weights[i]); err != nil {
			return err
		}
	}
	if len(other.centroids) > 0 {
		if d.count == 0 || other.min < d.min {
			d.min = other.min
		}
		if d.count == 0 || other.max > d.max {
			d.max = other.max
		}
		for _, c := range other.centroids {
			d.count += c.weight
		}
		d.centroids = append(d.centroids, other.centroids...)
		d.compress()
	}
	return nil
}

// sorted returns the digest's points in ascending order.
func (d *digest) sorted() []centroid {
	points := make([]centroid, 0, len(d.centroids)+len(d.exact))
	points = append(points, d.centroids...)
	for i, value := range d.exact {
		points = append(points, centroid{value, d.weights[i]})
	}
	sort.Slice(points, func(i, j int) bool { return points[i].mean < points[j].mean })
	return points
}

// percentile is the nearest-rank percentile: the smallest point whose
// cumulative weight reaches p% of the total; 0 is the minimum.
func (d *digest) percentile(p float64) (float64, bool) {
	if d.count == 0 || math.IsNaN(p) || p < 0 || p > 100 {
		return 0, false
	}
	if p == 0 {
		return d.min, true
	}
	if p == 100 {
		return d.max, true
	}
	rank := p / 100 * d.count
	cumulative := 0.0
	for _, point := range d.sorted() {
		cumulative += point.weight
		if cumulative >= rank-1e-9*d.count {
			return math.Min(math.Max(point.mean, d.min), d.max), true
		}
	}
	return d.max, true
}

// rank is the total weight of values less than or equal to value.
func (d *digest) rank(value float64) float64 {
	total := 0.0
	for _, point := range d.sorted() {
		if point.mean <= value {
			total += point.weight
		}
	}
	return total
}

type digestEncoding struct {
	Kind      string       `json:"tdigest"`
	Count     float64      `json:"n"`
	Min       float64      `json:"min"`
	Max       float64      `json:"max"`
	Points    [][2]float64 `json:"points"`
	Compacted bool         `json:"compacted,omitempty"`
}

func (d *digest) encode() (any, error) {
	if d.count == 0 {
		return nil, nil
	}
	encoding := digestEncoding{Kind: d.kind, Count: d.count, Min: d.min, Max: d.max, Compacted: len(d.centroids) > 0}
	for _, point := range d.sorted() {
		encoding.Points = append(encoding.Points, [2]float64{point.mean, point.weight})
	}
	encoded, err := json.Marshal(encoding)
	if err != nil {
		return nil, err
	}
	if len(encoded) > maxDigestEncoding {
		return nil, fmt.Errorf("tdigest exceeds %d bytes", maxDigestEncoding)
	}
	return string(encoded), nil
}

func decodeDigest(value any) (*digest, bool) {
	text, ok := textArgument(value)
	if !ok || sqliteNull(value) || len(text) > maxDigestEncoding {
		return nil, false
	}
	var encoding digestEncoding
	if json.Unmarshal([]byte(text), &encoding) != nil || encoding.Kind == "" || encoding.Count <= 0 {
		return nil, false
	}
	d := &digest{kind: encoding.Kind, count: encoding.Count, min: encoding.Min, max: encoding.Max}
	for _, point := range encoding.Points {
		if point[1] <= 0 || math.IsNaN(point[0]) || math.IsInf(point[0], 0) {
			return nil, false
		}
		if encoding.Compacted {
			d.centroids = append(d.centroids, centroid{point[0], point[1]})
		} else {
			d.exact = append(d.exact, point[0])
			d.weights = append(d.weights, point[1])
		}
	}
	return d, true
}

// percentileAggregate implements tdigest, percentile(s) and their weighted
// forms. The mode is "digest" (return the sketch), "single" (one
// percentile) or "array" (a JSON array of percentiles).
type percentileAggregate struct {
	mode        string
	digest      digest
	percentiles []float64
}

func percentileList(value any) ([]float64, bool) {
	text, ok := textArgument(value)
	if !ok {
		if p, ok := number(value); ok {
			return []float64{p}, true
		}
		return nil, false
	}
	var list []float64
	if json.Unmarshal([]byte(text), &list) != nil || len(list) == 0 || len(list) > 256 {
		return nil, false
	}
	for _, p := range list {
		if p < 0 || p > 100 {
			return nil, false
		}
	}
	return list, true
}

func (a *percentileAggregate) Step(value any, kind string, weightValue any, percentilesValue any) error {
	if a.percentiles == nil && a.mode != "digest" {
		list, ok := percentileList(percentilesValue)
		if !ok {
			return fmt.Errorf("percentiles must be constants between 0 and 100")
		}
		a.percentiles = list
	}
	a.digest.kind = kind
	number, ok := digestValue(value, kind)
	if !ok {
		return nil
	}
	weight := 1.0
	if weightValue != nil && !sqliteNull(weightValue) {
		w, ok := weightValue.(int64)
		if !ok {
			if real, isReal := weightValue.(float64); isReal && real == math.Trunc(real) {
				w, ok = int64(real), true
			}
		}
		if !ok || w < 0 {
			return fmt.Errorf("percentile weights must be non-negative integers")
		}
		weight = float64(w)
	}
	return a.digest.add(number, weight)
}

func (a *percentileAggregate) Done() (any, error) {
	switch a.mode {
	case "digest":
		return a.digest.encode()
	case "single":
		if len(a.percentiles) == 0 {
			return nil, nil
		}
		p, ok := a.digest.percentile(a.percentiles[0])
		if !ok {
			return nil, nil
		}
		return digestResult(p, a.digest.kind), nil
	}
	if a.digest.count == 0 {
		return nil, nil
	}
	results := make([]any, len(a.percentiles))
	for i, p := range a.percentiles {
		value, _ := a.digest.percentile(p)
		results[i] = digestResult(value, a.digest.kind)
	}
	encoded, err := json.Marshal(results)
	return string(encoded), err
}

// tdigestMerge merges sketches (the tdigest_merge aggregate).
type tdigestMerge struct{ digest digest }

func (a *tdigestMerge) Step(value any) error {
	other, ok := decodeDigest(value)
	if !ok {
		return nil
	}
	return a.digest.merge(other)
}

func (a *tdigestMerge) Done() (any, error) { return a.digest.encode() }

func kqlMergeTDigest(values ...any) (any, error) {
	merged := &digest{}
	for _, value := range values {
		other, ok := decodeDigest(value)
		if !ok {
			continue
		}
		if err := merged.merge(other); err != nil {
			return nil, err
		}
	}
	return merged.encode()
}

func kqlPercentileTDigest(value, percentileValue any) any {
	d, ok := decodeDigest(value)
	p, okP := number(percentileValue)
	if !ok || !okP {
		return nil
	}
	result, ok := d.percentile(p)
	if !ok {
		return nil
	}
	return digestResult(result, d.kind)
}

func kqlPercentilesArrayTDigest(value, percentilesValue any) (any, error) {
	d, ok := decodeDigest(value)
	list, okList := percentileList(percentilesValue)
	if !ok || !okList {
		return nil, nil
	}
	results := make([]any, len(list))
	for i, p := range list {
		result, _ := d.percentile(p)
		results[i] = digestResult(result, d.kind)
	}
	encoded, err := json.Marshal(results)
	return string(encoded), err
}

func kqlRankTDigest(value, target any) any {
	d, ok := decodeDigest(value)
	if !ok {
		return nil
	}
	number, ok := digestValue(target, d.kind)
	if !ok {
		return nil
	}
	return int64(math.Round(d.rank(number)))
}

func kqlPercentrankTDigest(value, target any) any {
	d, ok := decodeDigest(value)
	if !ok {
		return nil
	}
	number, ok := digestValue(target, d.kind)
	if !ok {
		return nil
	}
	return d.rank(number) * 100 / d.count
}

// HyperLogLog sketches keep exact hashes while small (so small distinct
// counts are exact) and 2^p registers beyond that. Kusto's accuracy levels
// 0..4 select p = 12, 14, 16, 17, 18.
var hllPrecision = []int{12, 14, 16, 17, 18}

type hllSketch struct {
	p         int
	sparse    map[uint64]struct{}
	registers []uint8
}

type hllEncoding struct {
	Precision int      `json:"hll"`
	Sparse    []string `json:"sparse,omitempty"`
	Dense     string   `json:"dense,omitempty"`
}

func hllHash(value any) uint64 {
	if bytes, ok := value.([]byte); ok {
		value = string(bytes)
	}
	var key string
	switch v := value.(type) {
	case string:
		key = "s" + v
	case int64:
		key = "i" + strconv.FormatInt(v, 10)
	case float64:
		if v == math.Trunc(v) && math.Abs(v) < 1<<53 {
			key = "i" + strconv.FormatInt(int64(v), 10)
		} else {
			key = "f" + strconv.FormatFloat(v, 'g', -1, 64)
		}
	case bool:
		key = "b" + strconv.FormatBool(v)
	case time.Time:
		key = "t" + v.UTC().Format(time.RFC3339Nano)
	default:
		key = fmt.Sprint(v)
	}
	hasher := fnv.New64a()
	hasher.Write([]byte(key))
	// splitmix64 finalization spreads FNV's low-entropy high bits.
	x := hasher.Sum64()
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	return x ^ x>>31
}

func newHLL(p int) *hllSketch { return &hllSketch{p: p, sparse: map[uint64]struct{}{}} }

func (h *hllSketch) addHash(hash uint64) {
	if h.registers == nil {
		h.sparse[hash] = struct{}{}
		if len(h.sparse) > maxHLLSparseHashes {
			h.densify()
		}
		return
	}
	index := hash >> (64 - h.p)
	rest := hash<<h.p | 1<<(h.p-1)
	rho := uint8(bits.LeadingZeros64(rest) + 1)
	if rho > h.registers[index] {
		h.registers[index] = rho
	}
}

func (h *hllSketch) densify() {
	h.registers = make([]uint8, 1<<h.p)
	sparse := h.sparse
	h.sparse = nil
	for hash := range sparse {
		h.addHash(hash)
	}
}

func (h *hllSketch) estimate() int64 {
	if h.registers == nil {
		return int64(len(h.sparse))
	}
	m := float64(len(h.registers))
	sum, zeros := 0.0, 0
	for _, register := range h.registers {
		sum += math.Ldexp(1, -int(register))
		if register == 0 {
			zeros++
		}
	}
	estimate := 0.7213 / (1 + 1.079/m) * m * m / sum
	if estimate <= 2.5*m && zeros > 0 {
		estimate = m * math.Log(m/float64(zeros))
	}
	return int64(math.Round(estimate))
}

func (h *hllSketch) merge(other *hllSketch) bool {
	if other.p != h.p {
		// Sketches of different accuracy merge only while both are exact.
		if h.registers != nil || other.registers != nil {
			return false
		}
		h.p = min(h.p, other.p)
	}
	if other.registers == nil {
		for hash := range other.sparse {
			h.addHash(hash)
		}
		return true
	}
	if h.registers == nil {
		h.densify()
	}
	for i, register := range other.registers {
		if register > h.registers[i] {
			h.registers[i] = register
		}
	}
	return true
}

func (h *hllSketch) encode() (any, error) {
	encoding := hllEncoding{Precision: h.p}
	if h.registers == nil {
		hashes := make([]uint64, 0, len(h.sparse))
		for hash := range h.sparse {
			hashes = append(hashes, hash)
		}
		sort.Slice(hashes, func(i, j int) bool { return hashes[i] < hashes[j] })
		encoding.Sparse = make([]string, len(hashes))
		for i, hash := range hashes {
			encoding.Sparse[i] = strconv.FormatUint(hash, 16)
		}
	} else {
		encoding.Dense = base64.StdEncoding.EncodeToString(h.registers)
	}
	encoded, err := json.Marshal(encoding)
	return string(encoded), err
}

func decodeHLL(value any) (*hllSketch, bool) {
	text, ok := textArgument(value)
	if !ok || sqliteNull(value) || len(text) > maxReplacementBytes {
		return nil, false
	}
	var encoding hllEncoding
	if json.Unmarshal([]byte(text), &encoding) != nil {
		return nil, false
	}
	valid := false
	for _, p := range hllPrecision {
		valid = valid || p == encoding.Precision
	}
	if !valid || len(encoding.Sparse) > maxHLLSparseHashes {
		return nil, false
	}
	h := newHLL(encoding.Precision)
	if encoding.Dense != "" {
		registers, err := base64.StdEncoding.DecodeString(encoding.Dense)
		if err != nil || len(registers) != 1<<encoding.Precision {
			return nil, false
		}
		h.sparse, h.registers = nil, registers
		return h, true
	}
	for _, text := range encoding.Sparse {
		hash, err := strconv.ParseUint(text, 16, 64)
		if err != nil {
			return nil, false
		}
		h.sparse[hash] = struct{}{}
	}
	return h, true
}

// hllAggregate builds a sketch (hll, hll_if) or merges sketches (hll_merge).
type hllAggregate struct {
	merge  bool
	sketch *hllSketch
}

func (a *hllAggregate) Step(value any, accuracyValue any) error {
	if a.merge {
		other, ok := decodeHLL(value)
		if !ok {
			return nil
		}
		if a.sketch == nil {
			a.sketch = newHLL(other.p)
		}
		if !a.sketch.merge(other) {
			return fmt.Errorf("hll_merge requires sketches of the same accuracy")
		}
		return nil
	}
	if a.sketch == nil {
		accuracy, ok := accuracyValue.(int64)
		if !ok || accuracy < 0 || accuracy > 4 {
			return fmt.Errorf("hll accuracy must be 0 to 4")
		}
		a.sketch = newHLL(hllPrecision[accuracy])
	}
	if value == nil || sqliteNull(value) {
		return nil
	}
	a.sketch.addHash(hllHash(value))
	return nil
}

func (a *hllAggregate) Done() (any, error) {
	if a.sketch == nil {
		return nil, nil
	}
	return a.sketch.encode()
}

func kqlHLLMerge(values ...any) (any, error) {
	var merged *hllSketch
	for _, value := range values {
		other, ok := decodeHLL(value)
		if !ok {
			continue
		}
		if merged == nil {
			merged = newHLL(other.p)
		}
		if !merged.merge(other) {
			return nil, nil
		}
	}
	if merged == nil {
		return nil, nil
	}
	return merged.encode()
}

func kqlDcountHLL(value any) any {
	sketch, ok := decodeHLL(value)
	if !ok {
		return nil
	}
	return sketch.estimate()
}
