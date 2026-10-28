package database

import (
	"encoding/json"
	"math"
	"strings"

	"github.com/golang/geo/r2"
	"github.com/golang/geo/s1"
	"github.com/golang/geo/s2"
)

// Kusto's geospatial functions measure on a sphere whose radius is S2's mean
// Earth radius; line and polygon edges are geodesics.
const (
	geoEarthRadius = 6371010.0
	// The densified result and geometry inputs are bounded so a single value
	// cannot exhaust memory.
	maxGeoVertices = 1 << 20
)

type geoCoordinate struct{ lng, lat float64 }

func (c geoCoordinate) point() s2.Point {
	return s2.PointFromLatLng(s2.LatLngFromDegrees(c.lat, c.lng))
}

func geoCoordinateOf(p s2.Point) geoCoordinate {
	ll := s2.LatLngFromPoint(p)
	return geoCoordinate{lng: ll.Lng.Degrees(), lat: ll.Lat.Degrees()}
}

func (c geoCoordinate) MarshalJSON() ([]byte, error) {
	return json.Marshal([2]float64{c.lng, c.lat})
}

func geoNumber(value any) (float64, bool) {
	switch number := value.(type) {
	case int64:
		return float64(number), true
	case float64:
		return number, !math.IsNaN(number) && !math.IsInf(number, 0)
	}
	return 0, false
}

func geoValidCoordinate(lng, lat float64) bool {
	return lng >= -180 && lng <= 180 && lat >= -90 && lat <= 90
}

func geoCoordinateArgs(lngValue, latValue any) (geoCoordinate, bool) {
	lng, okLng := geoNumber(lngValue)
	lat, okLat := geoNumber(latValue)
	if !okLng || !okLat || !geoValidCoordinate(lng, lat) {
		return geoCoordinate{}, false
	}
	return geoCoordinate{lng: lng, lat: lat}, true
}

func geoPointJSON(c geoCoordinate) (any, error) {
	return geoGeometryJSON("Point", c)
}

func geoGeometryJSON(kind string, coordinates any) (any, error) {
	encoded, err := json.Marshal(struct {
		Type        string `json:"type"`
		Coordinates any    `json:"coordinates"`
	}{kind, coordinates})
	if err != nil || len(encoded) > maxReplacementBytes {
		return nil, err
	}
	return string(encoded), nil
}

// geoGeometry is a decoded GeoJSON value. Lines hold LineString and
// MultiLineString paths; Polygons hold rings (shell first) per polygon.
type geoGeometry struct {
	kind     string
	points   []geoCoordinate
	lines    [][]geoCoordinate
	polygons [][][]geoCoordinate
}

type rawGeometry struct {
	Type        string            `json:"type"`
	Coordinates json.RawMessage   `json:"coordinates"`
	Geometries  []json.RawMessage `json:"geometries"`
}

func decodeCoordinate(raw []float64) (geoCoordinate, bool) {
	if len(raw) < 2 || !geoValidCoordinate(raw[0], raw[1]) {
		return geoCoordinate{}, false
	}
	return geoCoordinate{lng: raw[0], lat: raw[1]}, true
}

func decodeCoordinates(raw [][]float64) ([]geoCoordinate, bool) {
	if len(raw) > maxGeoVertices {
		return nil, false
	}
	result := make([]geoCoordinate, len(raw))
	for i, value := range raw {
		c, ok := decodeCoordinate(value)
		if !ok {
			return nil, false
		}
		result[i] = c
	}
	return result, true
}

// parseGeometry decodes and bounds a GeoJSON value; maxGeoVertices limits
// the vertices of all paths and rings together.
func parseGeometry(value any) (geoGeometry, bool) {
	geometry, ok := decodeGeometry(value)
	if !ok {
		return geoGeometry{}, false
	}
	total := len(geometry.points)
	for _, line := range geometry.lines {
		total += len(line)
	}
	for _, polygon := range geometry.polygons {
		for _, ring := range polygon {
			total += len(ring)
		}
	}
	return geometry, total <= maxGeoVertices
}

func decodeGeometry(value any) (geoGeometry, bool) {
	text, ok := textArgument(value)
	if !ok || sqliteNull(value) {
		return geoGeometry{}, false
	}
	var raw rawGeometry
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return geoGeometry{}, false
	}
	geometry := geoGeometry{kind: raw.Type}
	switch raw.Type {
	case "Point":
		var c []float64
		if json.Unmarshal(raw.Coordinates, &c) != nil {
			return geoGeometry{}, false
		}
		point, ok := decodeCoordinate(c)
		geometry.points = []geoCoordinate{point}
		return geometry, ok
	case "MultiPoint", "LineString":
		var cs [][]float64
		if json.Unmarshal(raw.Coordinates, &cs) != nil {
			return geoGeometry{}, false
		}
		points, ok := decodeCoordinates(cs)
		if !ok {
			return geoGeometry{}, false
		}
		if raw.Type == "MultiPoint" {
			geometry.points = points
		} else {
			geometry.lines = [][]geoCoordinate{points}
		}
		return geometry, true
	case "MultiLineString", "Polygon":
		var paths [][][]float64
		if json.Unmarshal(raw.Coordinates, &paths) != nil {
			return geoGeometry{}, false
		}
		var decoded [][]geoCoordinate
		for _, path := range paths {
			points, ok := decodeCoordinates(path)
			if !ok {
				return geoGeometry{}, false
			}
			decoded = append(decoded, points)
		}
		if raw.Type == "Polygon" {
			geometry.polygons = [][][]geoCoordinate{decoded}
		} else {
			geometry.lines = decoded
		}
		return geometry, true
	case "MultiPolygon":
		var polygons [][][][]float64
		if json.Unmarshal(raw.Coordinates, &polygons) != nil {
			return geoGeometry{}, false
		}
		for _, polygon := range polygons {
			var rings [][]geoCoordinate
			for _, ring := range polygon {
				points, ok := decodeCoordinates(ring)
				if !ok {
					return geoGeometry{}, false
				}
				rings = append(rings, points)
			}
			geometry.polygons = append(geometry.polygons, rings)
		}
		return geometry, true
	}
	return geoGeometry{}, false
}

// geoLines returns validated LineString/MultiLineString paths: two or more
// vertices each, and no edge of 180 degrees or more.
func geoLines(value any) ([][]s2.Point, [][]geoCoordinate, bool) {
	geometry, ok := parseGeometry(value)
	if !ok || (geometry.kind != "LineString" && geometry.kind != "MultiLineString") || len(geometry.lines) == 0 {
		return nil, nil, false
	}
	var lines [][]s2.Point
	for _, line := range geometry.lines {
		if len(line) < 2 {
			return nil, nil, false
		}
		points := make([]s2.Point, len(line))
		for i, c := range line {
			points[i] = c.point()
			if i > 0 && points[i-1].Angle(points[i].Vector) >= math.Pi-1e-12 {
				return nil, nil, false
			}
		}
		lines = append(lines, points)
	}
	return lines, geometry.lines, true
}

// geoLoop builds a loop from a closed GeoJSON ring, enclosing the smaller of
// the two regions the ring divides the sphere into.
func geoLoop(ring []geoCoordinate) (*s2.Loop, bool) {
	if len(ring) < 4 || ring[0] != ring[len(ring)-1] {
		return nil, false
	}
	points := make([]s2.Point, 0, len(ring)-1)
	for _, c := range ring[:len(ring)-1] {
		points = append(points, c.point())
	}
	loop := s2.LoopFromPoints(points)
	if loop.Validate() != nil {
		return nil, false
	}
	loop.Normalize()
	return loop, true
}

func geoPolygon(value any) (*s2.Polygon, geoGeometry, bool) {
	geometry, ok := parseGeometry(value)
	if !ok || (geometry.kind != "Polygon" && geometry.kind != "MultiPolygon") || len(geometry.polygons) == 0 {
		return nil, geometry, false
	}
	var loops []*s2.Loop
	for _, polygon := range geometry.polygons {
		if len(polygon) == 0 {
			return nil, geometry, false
		}
		for _, ring := range polygon {
			loop, ok := geoLoop(ring)
			if !ok {
				return nil, geometry, false
			}
			loops = append(loops, loop)
		}
	}
	polygon := s2.PolygonFromLoops(loops)
	if polygon.Validate() != nil {
		return nil, geometry, false
	}
	return polygon, geometry, true
}

func geoMeters(angle s1.Angle) float64 { return angle.Radians() * geoEarthRadius }

func kqlGeoDistance2Points(lng1, lat1, lng2, lat2, spheroid any) any {
	a, okA := geoCoordinateArgs(lng1, lat1)
	b, okB := geoCoordinateArgs(lng2, lat2)
	if !okA || !okB {
		return nil
	}
	flag, isBool := spheroid.(int64)
	if !isBool {
		return nil
	}
	if flag != 0 {
		if distance, ok := vincentyDistance(a, b); ok {
			return distance
		}
		return nil
	}
	return geoMeters(a.point().Distance(b.point()))
}

// vincentyDistance is the WGS-84 ellipsoidal distance (Vincenty's inverse
// formula); it reports false when the iteration does not converge, which
// happens only for nearly antipodal points.
func vincentyDistance(p, q geoCoordinate) (float64, bool) {
	const a, f = 6378137.0, 1 / 298.257223563
	b := a * (1 - f)
	toRad := math.Pi / 180
	L := (q.lng - p.lng) * toRad
	U1 := math.Atan((1 - f) * math.Tan(p.lat*toRad))
	U2 := math.Atan((1 - f) * math.Tan(q.lat*toRad))
	sinU1, cosU1 := math.Sincos(U1)
	sinU2, cosU2 := math.Sincos(U2)
	lambda := L
	var sinSigma, cosSigma, sigma, cosSqAlpha, cos2SigmaM float64
	for i := 0; ; i++ {
		if i == 200 {
			return 0, false
		}
		sinLambda, cosLambda := math.Sincos(lambda)
		sinSigma = math.Hypot(cosU2*sinLambda, cosU1*sinU2-sinU1*cosU2*cosLambda)
		if sinSigma == 0 {
			return 0, true
		}
		cosSigma = sinU1*sinU2 + cosU1*cosU2*cosLambda
		sigma = math.Atan2(sinSigma, cosSigma)
		sinAlpha := cosU1 * cosU2 * sinLambda / sinSigma
		cosSqAlpha = 1 - sinAlpha*sinAlpha
		cos2SigmaM = 0
		if cosSqAlpha != 0 {
			cos2SigmaM = cosSigma - 2*sinU1*sinU2/cosSqAlpha
		}
		C := f / 16 * cosSqAlpha * (4 + f*(4-3*cosSqAlpha))
		previous := lambda
		lambda = L + (1-C)*f*sinAlpha*(sigma+C*sinSigma*(cos2SigmaM+C*cosSigma*(-1+2*cos2SigmaM*cos2SigmaM)))
		if math.Abs(lambda-previous) < 1e-12 {
			break
		}
	}
	uSq := cosSqAlpha * (a*a - b*b) / (b * b)
	A := 1 + uSq/16384*(4096+uSq*(-768+uSq*(320-175*uSq)))
	B := uSq / 1024 * (256 + uSq*(-128+uSq*(74-47*uSq)))
	deltaSigma := B * sinSigma * (cos2SigmaM + B/4*(cosSigma*(-1+2*cos2SigmaM*cos2SigmaM)-B/6*cos2SigmaM*(-3+4*sinSigma*sinSigma)*(-3+4*cos2SigmaM*cos2SigmaM)))
	return b * A * (sigma - deltaSigma), true
}

// geoBearing is the initial great-circle bearing from p to q, clockwise
// from true north in [0, 2pi); it is undefined for equal or antipodal points.
func geoBearing(p, q geoCoordinate) (float64, bool) {
	a, b := p.point(), q.point()
	if a.ApproxEqual(b) || a.Angle(b.Vector) >= math.Pi-1e-12 {
		return 0, false
	}
	toRad := math.Pi / 180
	phi1, phi2 := p.lat*toRad, q.lat*toRad
	dLng := (q.lng - p.lng) * toRad
	y := math.Sin(dLng) * math.Cos(phi2)
	x := math.Cos(phi1)*math.Sin(phi2) - math.Sin(phi1)*math.Cos(phi2)*math.Cos(dLng)
	bearing := math.Atan2(y, x)
	if bearing < 0 {
		bearing += 2 * math.Pi
	}
	return bearing, true
}

func kqlGeoAzimuth(lng1, lat1, lng2, lat2 any) any {
	a, okA := geoCoordinateArgs(lng1, lat1)
	b, okB := geoCoordinateArgs(lng2, lat2)
	if !okA || !okB {
		return nil
	}
	if bearing, ok := geoBearing(a, b); ok {
		return bearing
	}
	return nil
}

// kqlGeoAngle measures clockwise at p2 from line [p1, p2] to line [p2, p3].
func kqlGeoAngle(lng1, lat1, lng2, lat2, lng3, lat3 any) any {
	p1, ok1 := geoCoordinateArgs(lng1, lat1)
	p2, ok2 := geoCoordinateArgs(lng2, lat2)
	p3, ok3 := geoCoordinateArgs(lng3, lat3)
	if !ok1 || !ok2 || !ok3 {
		return nil
	}
	back, okBack := geoBearing(p2, p1)
	forward, okForward := geoBearing(p2, p3)
	if !okBack || !okForward {
		return nil
	}
	return math.Mod(forward-back+4*math.Pi, 2*math.Pi)
}

func kqlGeoPointInCircle(lng, lat, centerLng, centerLat, radiusValue any) any {
	p, okP := geoCoordinateArgs(lng, lat)
	c, okC := geoCoordinateArgs(centerLng, centerLat)
	radius, okR := geoNumber(radiusValue)
	if !okP || !okC || !okR || radius <= 0 {
		return nil
	}
	return boolInt(geoMeters(p.point().Distance(c.point())) <= radius)
}

func boolInt(value bool) int64 {
	if value {
		return 1
	}
	return 0
}

func kqlGeoPointInPolygon(lng, lat, polygonValue any) any {
	p, okP := geoCoordinateArgs(lng, lat)
	polygon, _, okPolygon := geoPolygon(polygonValue)
	if !okP || !okPolygon {
		return nil
	}
	return boolInt(polygon.ContainsPoint(p.point()))
}

func lineLength(line []s2.Point) s1.Angle {
	var total s1.Angle
	for i := 1; i < len(line); i++ {
		total += line[i-1].Distance(line[i])
	}
	return total
}

func kqlGeoLineLength(value any) any {
	lines, _, ok := geoLines(value)
	if !ok {
		return nil
	}
	var total s1.Angle
	for _, line := range lines {
		total += lineLength(line)
	}
	return geoMeters(total)
}

func kqlGeoLineCentroid(value any) (any, error) {
	lines, _, ok := geoLines(value)
	if !ok {
		return nil, nil
	}
	var sum s2.Point
	for _, line := range lines {
		for i := 1; i < len(line); i++ {
			sum = s2.Point{Vector: sum.Add(s2.EdgeTrueCentroid(line[i-1], line[i]).Vector)}
		}
	}
	if sum.Norm() == 0 {
		return nil, nil
	}
	return geoPointJSON(geoCoordinateOf(s2.Point{Vector: sum.Normalize()}))
}

func kqlGeoPolygonArea(value any) any {
	polygon, ok := geoUnionPolygon(value)
	if !ok {
		return nil
	}
	return polygon.Area() * geoEarthRadius * geoEarthRadius
}

func kqlGeoPolygonPerimeter(value any) any {
	polygon, ok := geoUnionPolygon(value)
	if !ok {
		return nil
	}
	var total s1.Angle
	for _, loop := range polygon.Loops() {
		vertices := loop.Vertices()
		for i := range vertices {
			total += vertices[i].Distance(vertices[(i+1)%len(vertices)])
		}
	}
	return geoMeters(total)
}

func kqlGeoPolygonCentroid(value any) (any, error) {
	polygon, ok := geoUnionPolygon(value)
	if !ok {
		return nil, nil
	}
	centroid := polygon.Centroid()
	if centroid.Norm() == 0 {
		return nil, nil
	}
	return geoPointJSON(geoCoordinateOf(s2.Point{Vector: centroid.Normalize()}))
}

// closestOnLines returns the closest point of the lines to p and its
// distance.
func closestOnLines(lines [][]s2.Point, p s2.Point) (s2.Point, s1.Angle) {
	best, bestDistance := s2.Point{}, s1.InfAngle()
	for _, line := range lines {
		for i := 1; i < len(line); i++ {
			candidate := s2.Project(p, line[i-1], line[i])
			if distance := p.Distance(candidate); distance < bestDistance {
				best, bestDistance = candidate, distance
			}
		}
	}
	return best, bestDistance
}

// closestCoordinate reports a vertex the closest point coincides with by its
// input coordinates, avoiding a lossy round trip through S2 points.
func closestCoordinate(closest s2.Point, paths [][]geoCoordinate) geoCoordinate {
	for _, path := range paths {
		for _, c := range path {
			if c.point() == closest {
				return c
			}
		}
	}
	return geoCoordinateOf(closest)
}

func polygonBoundary(polygon *s2.Polygon) [][]s2.Point {
	var lines [][]s2.Point
	for _, loop := range polygon.Loops() {
		vertices := loop.Vertices()
		lines = append(lines, append(append([]s2.Point{}, vertices...), vertices[0]))
	}
	return lines
}

func kqlGeoDistancePointToLine(lng, lat, lineValue any) any {
	p, okP := geoCoordinateArgs(lng, lat)
	lines, _, okLines := geoLines(lineValue)
	if !okP || !okLines {
		return nil
	}
	_, distance := closestOnLines(lines, p.point())
	return geoMeters(distance)
}

func kqlGeoClosestPointOnLine(lng, lat, lineValue any) (any, error) {
	p, okP := geoCoordinateArgs(lng, lat)
	lines, paths, okLines := geoLines(lineValue)
	if !okP || !okLines {
		return nil, nil
	}
	closest, _ := closestOnLines(lines, p.point())
	return geoPointJSON(closestCoordinate(closest, paths))
}

func kqlGeoDistancePointToPolygon(lng, lat, polygonValue any) any {
	p, okP := geoCoordinateArgs(lng, lat)
	polygon, _, okPolygon := geoPolygon(polygonValue)
	if !okP || !okPolygon {
		return nil
	}
	if polygon.ContainsPoint(p.point()) {
		return 0.0
	}
	_, distance := closestOnLines(polygonBoundary(polygon), p.point())
	return geoMeters(distance)
}

func kqlGeoClosestPointOnPolygon(lng, lat, polygonValue any) (any, error) {
	p, okP := geoCoordinateArgs(lng, lat)
	polygon, geometry, okPolygon := geoPolygon(polygonValue)
	if !okP || !okPolygon {
		return nil, nil
	}
	if polygon.ContainsPoint(p.point()) {
		return geoPointJSON(p)
	}
	closest, _ := closestOnLines(polygonBoundary(polygon), p.point())
	var rings [][]geoCoordinate
	for _, polygon := range geometry.polygons {
		rings = append(rings, polygon...)
	}
	return geoPointJSON(closestCoordinate(closest, rings))
}

// geoLinePoints joins a LineString's paths for interpolation; it requires a
// single path, as Kusto's line_interpolate/locate functions do.
func geoSingleLine(value any) (s2.Polyline, bool) {
	lines, _, ok := geoLines(value)
	if !ok || len(lines) != 1 {
		return nil, false
	}
	return s2.Polyline(lines[0]), true
}

func kqlGeoLineInterpolatePoint(lineValue, fractionValue any) (any, error) {
	line, ok := geoSingleLine(lineValue)
	fraction, okFraction := geoNumber(fractionValue)
	if !ok || !okFraction || fraction < 0 || fraction > 1 {
		return nil, nil
	}
	point, _ := line.Interpolate(fraction)
	return geoPointJSON(geoCoordinateOf(point))
}

func kqlGeoLineLocatePoint(lineValue, lng, lat any) any {
	line, ok := geoSingleLine(lineValue)
	p, okP := geoCoordinateArgs(lng, lat)
	if !ok || !okP {
		return nil
	}
	point, next := line.Project(p.point())
	return line.Uninterpolate(point, next)
}

// geoTolerance reads a tolerance in meters within Kusto's supported range.
func geoTolerance(value any, low, high float64) (float64, bool) {
	tolerance, ok := geoNumber(value)
	return tolerance, ok && tolerance >= low && tolerance <= high
}

// densifyEdge splits a planar (longitude/latitude) edge in half until each
// piece is within the tolerance of its geodesic, as S2's EdgeTessellator
// does for an unprojected plate carrée edge, keeping planar coordinates.
func densifyEdge(a, b r2.Point, tolerance s1.ChordAngle, depth int, out []geoCoordinate) ([]geoCoordinate, bool) {
	if len(out) >= maxGeoVertices {
		return out, false
	}
	pa, pb := geoCoordinate{lng: a.X, lat: a.Y}.point(), geoCoordinate{lng: b.X, lat: b.Y}.point()
	if depth >= 45 || densifyError(a, pa, b, pb) <= tolerance {
		return append(out, geoCoordinate{lng: b.X, lat: b.Y}), true
	}
	mid := r2.Point{X: (a.X + b.X) / 2, Y: (a.Y + b.Y) / 2}
	out, ok := densifyEdge(a, mid, tolerance, depth+1, out)
	if !ok {
		return out, false
	}
	return densifyEdge(mid, b, tolerance, depth+1, out)
}

// tessellationFraction is S2's interpolation fraction for estimating the
// maximum distance between a planar edge and its geodesic.
const tessellationFraction = 0.31215691082248315

func densifyError(a r2.Point, pa s2.Point, b r2.Point, pb s2.Point) s1.ChordAngle {
	if pa.Dot(pb.Vector) < -1e-14 {
		return s1.InfChordAngle()
	}
	worst := s1.ChordAngle(0)
	for _, t := range []float64{tessellationFraction, 1 - tessellationFraction} {
		geodesic := s2.Interpolate(t, pa, pb)
		planar := geoCoordinate{lng: a.X + t*(b.X-a.X), lat: a.Y + t*(b.Y-a.Y)}.point()
		worst = max(worst, s2.ChordAngleBetweenPoints(geodesic, planar))
	}
	return worst
}

// The constant 0.6 scales the tolerance as S2's EdgeTessellator does (its
// error estimate can be off by up to that factor).
func densifyTolerance(meters float64) s1.ChordAngle {
	return s1.ChordAngleFromAngle(s1.Angle(meters/geoEarthRadius) * 0.6)
}

func densifyPath(path []geoCoordinate, tolerance s1.ChordAngle) ([]geoCoordinate, bool) {
	out := []geoCoordinate{path[0]}
	for i := 1; i < len(path); i++ {
		var ok bool
		out, ok = densifyEdge(r2.Point{X: path[i-1].lng, Y: path[i-1].lat}, r2.Point{X: path[i].lng, Y: path[i].lat}, tolerance, 0, out)
		if !ok {
			return nil, false
		}
	}
	return out, true
}

func kqlGeoLineDensify(lineValue, toleranceValue, _ any) (any, error) {
	_, paths, ok := geoLines(lineValue)
	meters, okTolerance := geoTolerance(toleranceValue, 0.1, 10000)
	if !ok || !okTolerance {
		return nil, nil
	}
	tolerance := densifyTolerance(meters)
	var result [][]geoCoordinate
	for _, path := range paths {
		densified, ok := densifyPath(path, tolerance)
		if !ok {
			return nil, nil
		}
		result = append(result, densified)
	}
	geometry, _ := parseGeometry(lineValue)
	if geometry.kind == "LineString" {
		return geoGeometryJSON("LineString", result[0])
	}
	return geoGeometryJSON("MultiLineString", result)
}

func kqlGeoPolygonDensify(polygonValue, toleranceValue, _ any) (any, error) {
	_, geometry, ok := geoPolygon(polygonValue)
	meters, okTolerance := geoTolerance(toleranceValue, 0.1, 10000)
	if !ok || !okTolerance {
		return nil, nil
	}
	tolerance := densifyTolerance(meters)
	var polygons [][][]geoCoordinate
	for _, polygon := range geometry.polygons {
		var rings [][]geoCoordinate
		for _, ring := range polygon {
			densified, ok := densifyPath(ring, tolerance)
			if !ok {
				return nil, nil
			}
			rings = append(rings, densified)
		}
		polygons = append(polygons, rings)
	}
	if geometry.kind == "Polygon" {
		return geoGeometryJSON("Polygon", polygons[0])
	}
	return geoGeometryJSON("MultiPolygon", polygons)
}

// simplifyPath keeps the vertices S2's SubsampleVertices selects: each
// dropped vertex lies within the tolerance of the simplified edge.
func simplifyPath(path []geoCoordinate, tolerance s1.Angle) []geoCoordinate {
	points := make(s2.Polyline, len(path))
	for i, c := range path {
		points[i] = c.point()
	}
	indexes := points.SubsampleVertices(tolerance)
	result := make([]geoCoordinate, len(indexes))
	for i, index := range indexes {
		result[i] = path[index]
	}
	return result
}

func kqlGeoLineSimplify(lineValue, toleranceValue any) (any, error) {
	lines, paths, ok := geoLines(lineValue)
	meters, okTolerance := geoTolerance(toleranceValue, 0, math.MaxFloat64)
	if !ok || !okTolerance {
		return nil, nil
	}
	tolerance := s1.Angle(meters / geoEarthRadius)
	var result [][]geoCoordinate
	for i, path := range paths {
		simplified := simplifyPath(path, tolerance)
		// A line whose length is within the tolerance collapses to a point.
		if len(simplified) < 2 || lineLength(lines[i]) <= tolerance {
			continue
		}
		result = append(result, simplified)
	}
	switch {
	case len(result) == 0:
		return nil, nil
	case len(result) == 1:
		return geoGeometryJSON("LineString", result[0])
	}
	return geoGeometryJSON("MultiLineString", result)
}

func kqlGeoPolygonSimplify(polygonValue, toleranceValue any) (any, error) {
	_, geometry, ok := geoPolygon(polygonValue)
	meters, okTolerance := geoTolerance(toleranceValue, 0, math.MaxFloat64)
	if !ok || !okTolerance {
		return nil, nil
	}
	tolerance := s1.Angle(meters / geoEarthRadius)
	var polygons [][][]geoCoordinate
	for _, polygon := range geometry.polygons {
		var rings [][]geoCoordinate
		for r, ring := range polygon {
			simplified := simplifyPath(ring, tolerance)
			if len(simplified) < 4 {
				// A collapsed shell drops its polygon; a collapsed hole is removed.
				if r == 0 {
					rings = nil
					break
				}
				continue
			}
			rings = append(rings, simplified)
		}
		if rings != nil {
			polygons = append(polygons, rings)
		}
	}
	switch {
	case len(polygons) == 0:
		return nil, nil
	case len(polygons) == 1:
		return geoGeometryJSON("Polygon", polygons[0])
	}
	return geoGeometryJSON("MultiPolygon", polygons)
}

// Lines intersect when any pair of their edges cross or touch.
func linesIntersect(a, b [][]s2.Point) bool {
	for _, first := range a {
		for i := 1; i < len(first); i++ {
			for _, second := range b {
				for j := 1; j < len(second); j++ {
					if s2.CrossingSign(first[i-1], first[i], second[j-1], second[j]) != s2.DoNotCross ||
						s2.VertexCrossing(first[i-1], first[i], second[j-1], second[j]) {
						return true
					}
					if touches(first[i-1], first[i], second[j-1], second[j]) {
						return true
					}
				}
			}
		}
	}
	return false
}

// touches reports an endpoint of one edge lying on the other.
func touches(a0, a1, b0, b1 s2.Point) bool {
	const epsilon = 1e-15
	return s2.DistanceFromSegment(a0, b0, b1) <= epsilon || s2.DistanceFromSegment(a1, b0, b1) <= epsilon ||
		s2.DistanceFromSegment(b0, a0, a1) <= epsilon || s2.DistanceFromSegment(b1, a0, a1) <= epsilon
}

func kqlGeoIntersects2Lines(first, second any) any {
	a, _, okA := geoLines(first)
	b, _, okB := geoLines(second)
	if !okA || !okB {
		return nil
	}
	return boolInt(linesIntersect(a, b))
}

func kqlGeoIntersectsLineWithPolygon(lineValue, polygonValue any) any {
	lines, _, okLines := geoLines(lineValue)
	polygon, _, okPolygon := geoPolygon(polygonValue)
	if !okLines || !okPolygon {
		return nil
	}
	for _, line := range lines {
		for _, vertex := range line {
			if polygon.ContainsPoint(vertex) {
				return int64(1)
			}
		}
	}
	return boolInt(linesIntersect(lines, polygonBoundary(polygon)))
}

func kqlGeoIntersects2Polygons(first, second any) any {
	a, _, okA := geoPolygon(first)
	b, _, okB := geoPolygon(second)
	if !okA || !okB {
		return nil
	}
	return boolInt(a.Intersects(b))
}

// Geohash cells use the standard base-32 alphabet; accuracy is 1..18.
const geohashAlphabet = "0123456789bcdefghjkmnpqrstuvwxyz"

func kqlGeoPointToGeohash(lngValue, latValue, accuracyValue any) any {
	c, ok := geoCoordinateArgs(lngValue, latValue)
	accuracy, isInt := accuracyValue.(int64)
	if !ok || !isInt || accuracy < 1 || accuracy > 18 {
		return nil
	}
	lngRange, latRange := [2]float64{-180, 180}, [2]float64{-90, 90}
	var builder strings.Builder
	bit, index, even := 0, 0, true
	for builder.Len() < int(accuracy) {
		ranges, value := &latRange, c.lat
		if even {
			ranges, value = &lngRange, c.lng
		}
		mid := (ranges[0] + ranges[1]) / 2
		index <<= 1
		if value >= mid {
			index |= 1
			ranges[0] = mid
		} else {
			ranges[1] = mid
		}
		even = !even
		if bit++; bit == 5 {
			builder.WriteByte(geohashAlphabet[index])
			bit, index = 0, 0
		}
	}
	return builder.String()
}

// geohashBounds decodes a geohash to its longitude and latitude ranges.
func geohashBounds(value any) ([2]float64, [2]float64, bool) {
	text, ok := textArgument(value)
	if !ok || sqliteNull(value) || len(text) == 0 || len(text) > 18 {
		return [2]float64{}, [2]float64{}, false
	}
	lngRange, latRange := [2]float64{-180, 180}, [2]float64{-90, 90}
	even := true
	for _, char := range strings.ToLower(text) {
		index := strings.IndexRune(geohashAlphabet, char)
		if index < 0 {
			return [2]float64{}, [2]float64{}, false
		}
		for shift := 4; shift >= 0; shift-- {
			ranges := &latRange
			if even {
				ranges = &lngRange
			}
			mid := (ranges[0] + ranges[1]) / 2
			if index>>shift&1 == 1 {
				ranges[0] = mid
			} else {
				ranges[1] = mid
			}
			even = !even
		}
	}
	return lngRange, latRange, true
}

func kqlGeoGeohashToCentralPoint(value any) (any, error) {
	lng, lat, ok := geohashBounds(value)
	if !ok {
		return nil, nil
	}
	return geoPointJSON(geoCoordinate{lng: (lng[0] + lng[1]) / 2, lat: (lat[0] + lat[1]) / 2})
}

func rectangleRing(lng, lat [2]float64) [][]geoCoordinate {
	return [][]geoCoordinate{{{lng[0], lat[0]}, {lng[1], lat[0]}, {lng[1], lat[1]}, {lng[0], lat[1]}, {lng[0], lat[0]}}}
}

func kqlGeoGeohashToPolygon(value any) (any, error) {
	lng, lat, ok := geohashBounds(value)
	if !ok {
		return nil, nil
	}
	return geoGeometryJSON("Polygon", rectangleRing(lng, lat))
}

// kqlGeoGeohashNeighbors lists the eight surrounding cells of the same
// precision, wrapping across the antimeridian and omitting cells past a pole.
func kqlGeoGeohashNeighbors(value any) (any, error) {
	lng, lat, ok := geohashBounds(value)
	if !ok {
		return nil, nil
	}
	text, _ := textArgument(value)
	width, height := lng[1]-lng[0], lat[1]-lat[0]
	centerLng, centerLat := (lng[0]+lng[1])/2, (lat[0]+lat[1])/2
	neighbors := []string{}
	for _, offset := range [][2]float64{{0, 1}, {1, 1}, {1, 0}, {1, -1}, {0, -1}, {-1, -1}, {-1, 0}, {-1, 1}} {
		y := centerLat + offset[1]*height
		if y > 90 || y < -90 {
			continue
		}
		x := centerLng + offset[0]*width
		if x > 180 {
			x -= 360
		} else if x < -180 {
			x += 360
		}
		if cell, ok := kqlGeoPointToGeohash(x, y, int64(len(text))).(string); ok {
			neighbors = append(neighbors, cell)
		}
	}
	encoded, err := json.Marshal(neighbors)
	return string(encoded), err
}

func s2CellArgument(value any) (s2.CellID, bool) {
	text, ok := textArgument(value)
	if !ok || sqliteNull(value) || len(text) == 0 || len(text) > 16 {
		return 0, false
	}
	id := s2.CellIDFromToken(text)
	return id, id.IsValid()
}

func kqlGeoPointToS2Cell(lngValue, latValue, levelValue any) any {
	c, ok := geoCoordinateArgs(lngValue, latValue)
	level, isInt := levelValue.(int64)
	if !ok || !isInt || level < 0 || level > 30 {
		return nil
	}
	return s2.CellIDFromLatLng(s2.LatLngFromDegrees(c.lat, c.lng)).Parent(int(level)).ToToken()
}

func kqlGeoS2CellToCentralPoint(value any) (any, error) {
	id, ok := s2CellArgument(value)
	if !ok {
		return nil, nil
	}
	return geoPointJSON(geoCoordinateOf(id.Point()))
}

func kqlGeoS2CellToPolygon(value any) (any, error) {
	id, ok := s2CellArgument(value)
	if !ok {
		return nil, nil
	}
	cell := s2.CellFromCellID(id)
	ring := make([]geoCoordinate, 0, 5)
	for k := 0; k < 4; k++ {
		ring = append(ring, geoCoordinateOf(cell.Vertex(k)))
	}
	ring = append(ring, ring[0])
	return geoGeometryJSON("Polygon", [][]geoCoordinate{ring})
}

func kqlGeoS2CellNeighbors(value any) (any, error) {
	id, ok := s2CellArgument(value)
	if !ok {
		return nil, nil
	}
	tokens := []string{}
	for _, neighbor := range id.AllNeighbors(id.Level()) {
		tokens = append(tokens, neighbor.ToToken())
	}
	encoded, err := json.Marshal(tokens)
	return string(encoded), err
}

// maxS2Covering is Kusto's limit on geo_polygon_to_s2cells tokens.
const maxS2Covering = 65536

// kqlGeoPolygonToS2Cells covers a polygon, and every point within radius
// meters of it, with cells of one level.
func kqlGeoPolygonToS2Cells(polygonValue, levelValue, radiusValue any) (any, error) {
	polygon, _, ok := geoPolygon(polygonValue)
	level, okLevel := levelValue.(int64)
	radius, okRadius := geoNumber(radiusValue)
	if !ok || !okLevel || !okRadius || level < 0 || level > 30 || radius < 0 {
		return nil, nil
	}
	// Reject coverings that cannot fit before computing them.
	cellArea := s2.AvgAreaMetric.Value(int(level))
	buffer := radius / geoEarthRadius
	estimate := (polygon.Area() + buffer*buffer*math.Pi + buffer*perimeterAngle(polygon)) / cellArea
	if estimate > 4*maxS2Covering {
		return nil, nil
	}
	coverer := &s2.RegionCoverer{MinLevel: int(level), MaxLevel: int(level), MaxCells: maxS2Covering}
	covering := coverer.Covering(polygon)
	if radius > 0 {
		var ok bool
		if covering, ok = bufferCovering(polygon, covering, int(level), s1.Angle(buffer)); !ok {
			return nil, nil
		}
	}
	if len(covering) > maxS2Covering {
		return nil, nil
	}
	tokens := make([]string, len(covering))
	for i, id := range covering {
		tokens[i] = id.ToToken()
	}
	encoded, err := json.Marshal(tokens)
	return string(encoded), err
}

func perimeterAngle(polygon *s2.Polygon) float64 {
	var total s1.Angle
	for _, loop := range polygon.Loops() {
		vertices := loop.Vertices()
		for i := range vertices {
			total += vertices[i].Distance(vertices[(i+1)%len(vertices)])
		}
	}
	return total.Radians()
}

// bufferCovering grows a covering by the cells within radius of the polygon,
// walking outward from the covered cells through their neighbors.
func bufferCovering(polygon *s2.Polygon, covering s2.CellUnion, level int, radius s1.Angle) (s2.CellUnion, bool) {
	index := s2.NewShapeIndex()
	index.Add(polygon)
	query := s2.NewClosestEdgeQuery(index, s2.NewClosestEdgeQueryOptions().IncludeInteriors(true))
	limit := s1.ChordAngleFromAngle(radius)
	seen := map[s2.CellID]bool{}
	queue := []s2.CellID{}
	for _, id := range covering {
		seen[id] = true
		queue = append(queue, id)
	}
	// A degenerate polygon can have an empty covering; seed from a vertex.
	if len(queue) == 0 && polygon.NumLoops() > 0 {
		id := s2.CellFromPoint(polygon.Loop(0).Vertex(0)).ID().Parent(level)
		seen[id] = true
		queue = append(queue, id)
		covering = append(covering, id)
	}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, neighbor := range id.AllNeighbors(level) {
			if seen[neighbor] {
				continue
			}
			seen[neighbor] = true
			if query.IsConservativeDistanceLessOrEqual(s2.NewMinDistanceToCellTarget(s2.CellFromCellID(neighbor)), limit) {
				covering = append(covering, neighbor)
				if len(covering) > maxS2Covering {
					return nil, false
				}
				queue = append(queue, neighbor)
			}
		}
	}
	covering.Normalize()
	// Normalize merges complete sibling sets; Kusto reports one level.
	var result s2.CellUnion
	for _, id := range covering {
		if id.Level() == level {
			result = append(result, id)
			continue
		}
		for child := id.ChildBeginAtLevel(level); child != id.ChildEndAtLevel(level); child = child.Next() {
			result = append(result, child)
		}
	}
	return result, true
}

// kqlGeoFromWKT converts Well-Known Text to GeoJSON.
func kqlGeoFromWKT(value any) (any, error) {
	text, ok := textArgument(value)
	if !ok || sqliteNull(value) || len(text) > maxReplacementBytes {
		return nil, nil
	}
	parser := &wktParser{text: text}
	geometry, ok := parser.geometry(0)
	parser.space()
	if !ok || parser.pos != len(parser.text) {
		return nil, nil
	}
	encoded, err := json.Marshal(geometry)
	if err != nil || len(encoded) > maxReplacementBytes {
		return nil, err
	}
	return string(encoded), nil
}

type wktParser struct {
	text string
	pos  int
}

type wktGeometry struct {
	Type        string        `json:"type"`
	Coordinates any           `json:"coordinates,omitempty"`
	Geometries  []wktGeometry `json:"geometries,omitempty"`
}

func (p *wktParser) space() {
	for p.pos < len(p.text) && strings.ContainsRune(" \t\r\n", rune(p.text[p.pos])) {
		p.pos++
	}
}

func (p *wktParser) word() string {
	p.space()
	start := p.pos
	for p.pos < len(p.text) && (p.text[p.pos] >= 'A' && p.text[p.pos] <= 'Z' || p.text[p.pos] >= 'a' && p.text[p.pos] <= 'z') {
		p.pos++
	}
	return strings.ToUpper(p.text[start:p.pos])
}

func (p *wktParser) consume(char byte) bool {
	p.space()
	if p.pos < len(p.text) && p.text[p.pos] == char {
		p.pos++
		return true
	}
	return false
}

func (p *wktParser) number() (float64, bool) {
	p.space()
	start := p.pos
	for p.pos < len(p.text) && strings.ContainsRune("+-.0123456789eE", rune(p.text[p.pos])) {
		p.pos++
	}
	var number float64
	if start == p.pos || json.Unmarshal([]byte(strings.TrimPrefix(p.text[start:p.pos], "+")), &number) != nil {
		return 0, false
	}
	return number, true
}

// coordinate reads "lng lat" with optional Z/M ordinates, which GeoJSON
// output drops.
func (p *wktParser) coordinate() ([]float64, bool) {
	lng, okLng := p.number()
	lat, okLat := p.number()
	if !okLng || !okLat || !geoValidCoordinate(lng, lat) {
		return nil, false
	}
	for i := 0; i < 2; i++ {
		saved := p.pos
		if _, ok := p.number(); !ok {
			p.pos = saved
			break
		}
	}
	return []float64{lng, lat}, true
}

// list parses "(" item {"," item} ")".
func (p *wktParser) list(item func() (any, bool)) ([]any, bool) {
	if !p.consume('(') {
		return nil, false
	}
	var items []any
	for {
		value, ok := item()
		if !ok || len(items) >= maxGeoVertices {
			return nil, false
		}
		items = append(items, value)
		if p.consume(')') {
			return items, true
		}
		if !p.consume(',') {
			return nil, false
		}
	}
}

func (p *wktParser) coordinates() (any, bool) {
	return p.list(func() (any, bool) { return p.coordinate() })
}

func (p *wktParser) rings() (any, bool) { return p.list(p.coordinates) }

func (p *wktParser) geometry(depth int) (wktGeometry, bool) {
	if depth > 32 {
		return wktGeometry{}, false
	}
	kind := p.word()
	if dimension := p.word(); dimension != "" && dimension != "Z" && dimension != "M" && dimension != "ZM" {
		return wktGeometry{}, false
	}
	var coordinates any
	var ok bool
	switch kind {
	case "POINT":
		if !p.consume('(') {
			return wktGeometry{}, false
		}
		coordinates, ok = p.coordinate()
		ok = ok && p.consume(')')
		return wktGeometry{Type: "Point", Coordinates: coordinates}, ok
	case "LINESTRING":
		coordinates, ok = p.coordinates()
		return wktGeometry{Type: "LineString", Coordinates: coordinates}, ok
	case "POLYGON":
		coordinates, ok = p.rings()
		return wktGeometry{Type: "Polygon", Coordinates: coordinates}, ok
	case "MULTIPOINT":
		// Points may be written bare or parenthesized.
		coordinates, ok = p.list(func() (any, bool) {
			if p.consume('(') {
				c, ok := p.coordinate()
				return c, ok && p.consume(')')
			}
			return p.coordinate()
		})
		return wktGeometry{Type: "MultiPoint", Coordinates: coordinates}, ok
	case "MULTILINESTRING":
		coordinates, ok = p.rings()
		return wktGeometry{Type: "MultiLineString", Coordinates: coordinates}, ok
	case "MULTIPOLYGON":
		coordinates, ok = p.list(p.rings)
		return wktGeometry{Type: "MultiPolygon", Coordinates: coordinates}, ok
	case "GEOMETRYCOLLECTION":
		var geometries []wktGeometry
		_, ok = p.list(func() (any, bool) {
			geometry, ok := p.geometry(depth + 1)
			geometries = append(geometries, geometry)
			return nil, ok
		})
		return wktGeometry{Type: "GeometryCollection", Geometries: geometries}, ok
	}
	return wktGeometry{}, false
}
