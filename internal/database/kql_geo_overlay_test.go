package database

import (
	"encoding/json"
	"math/rand"
	"testing"
)

func randomPolygon(r *rand.Rand) string {
	cx, cy := r.Float64()*4-2, r.Float64()*4-2
	w, h := 0.5+r.Float64()*2, 0.5+r.Float64()*2
	if r.Intn(2) == 0 {
		// Grid-aligned rectangles share and overlap edges exactly.
		cx, cy, w, h = float64(r.Intn(4)-2), float64(r.Intn(4)-2), float64(1+r.Intn(3)), float64(1+r.Intn(3))
	}
	ring := [][2]float64{{cx, cy}, {cx + w, cy}, {cx + w, cy + h}, {cx, cy + h}, {cx, cy}}
	encoded, _ := json.Marshal(map[string]any{"type": "Polygon", "coordinates": [][][2]float64{ring}})
	return string(encoded)
}

func polygonArea(t *testing.T, value any) float64 {
	t.Helper()
	text, _ := value.(string)
	if text == "" || text == `{"type":"GeometryCollection","geometries":[]}` {
		return 0
	}
	area, ok := kqlGeoPolygonArea(text).(float64)
	if !ok {
		var decoded map[string]any
		_ = json.Unmarshal([]byte(text), &decoded)
		switch decoded["type"] {
		case "Point", "MultiPoint", "LineString", "MultiLineString":
			return 0
		case "GeometryCollection":
			total := 0.0
			for _, geometry := range decoded["geometries"].([]any) {
				encoded, _ := json.Marshal(geometry)
				total += polygonArea(t, string(encoded))
			}
			return total
		}
		if false {
			return 0
		}
		t.Fatalf("result is not a measurable polygon: %s", text)
	}
	return area
}

// Random rectangles: overlays never panic, and the intersection's area is at
// most each input's, the union's at least each input's.
func TestOverlayAreaBounds(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	for i := 0; i < 600; i++ {
		a, b := randomPolygon(r), randomPolygon(r)
		areaA, areaB := kqlGeoPolygonArea(a).(float64), kqlGeoPolygonArea(b).(float64)
		intersection, err := kqlGeoIntersection2Polygons(a, b)
		if err != nil {
			t.Fatal(err)
		}
		pair, _ := json.Marshal([]json.RawMessage{json.RawMessage(a), json.RawMessage(b)})
		union, err := kqlGeoUnionPolygonsArray(string(pair))
		if err != nil {
			t.Fatal(err)
		}
		inter, all := polygonArea(t, intersection), polygonArea(t, union)
		tolerance := 1e-6 * (areaA + areaB)
		if inter > areaA+tolerance || inter > areaB+tolerance || all+tolerance < areaA || all+tolerance < areaB || all > areaA+areaB+tolerance {
			t.Fatalf("%s and %s: intersection %g, union %g, areas %g %g", a, b, inter, all, areaA, areaB)
		}
		// Inclusion-exclusion holds up to rounding.
		if diff := (areaA + areaB) - (inter + all); diff > 1e-3*(areaA+areaB) || diff < -1e-3*(areaA+areaB) {
			t.Fatalf("%s and %s: |A|+|B|=%g, |A∩B|+|A∪B|=%g", a, b, areaA+areaB, inter+all)
		}
	}
}
