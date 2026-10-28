package database

import (
	"encoding/json"
	"math"
	"sort"

	"github.com/golang/geo/s2"
)

// The overlay computes polygon intersections and unions on the sphere: both
// boundaries are split where they cross, each piece is kept when it lies
// inside (intersection) or outside (union) the other polygon, shared edges
// are kept once when both polygons lie on the same side, and the kept
// directed edges are chained back into loops.

// recoverGeometry turns a panic in geometry code (such as an S2 assertion on
// degenerate input) into a null result instead of crashing the process.
func recoverGeometry(result *any, err *error) {
	if recover() != nil {
		*result, *err = nil, nil
	}
}

// overlayEpsilon is the distance (radians, about 6 mm on Earth) under which
// a point counts as lying on a boundary.
const overlayEpsilon = 1e-9

// maxOverlayEdges bounds the work of one overlay.
const maxOverlayEdges = 1 << 16

type overlayEdge struct{ from, to s2.Point }

// boundaryEdges lists a polygon's directed boundary edges; S2 orients each
// loop with the polygon's interior on its left.
func boundaryEdges(polygon *s2.Polygon) []overlayEdge {
	var edges []overlayEdge
	for _, loop := range polygon.Loops() {
		vertices := loop.Vertices()
		for i := range vertices {
			edges = append(edges, overlayEdge{vertices[i], vertices[(i+1)%len(vertices)]})
		}
	}
	return edges
}

// splitEdges splits each edge of a at its crossings with the edges of b and
// at b's vertices lying on it.
func splitEdges(a, b []overlayEdge) []overlayEdge {
	var pieces []overlayEdge
	for _, edge := range a {
		splits := []s2.Point{}
		for _, other := range b {
			if s2.CrossingSign(edge.from, edge.to, other.from, other.to) == s2.Cross {
				// A crossing at (or next to) a vertex snaps to that vertex, so
				// both polygons split at the identical point.
				crossing := s2.Intersection(edge.from, edge.to, other.from, other.to)
				for _, vertex := range []s2.Point{other.from, other.to, edge.from, edge.to} {
					if crossing.Distance(vertex) < overlayEpsilon {
						crossing = vertex
					}
				}
				splits = append(splits, crossing)
			}
			for _, vertex := range []s2.Point{other.from, other.to} {
				if vertex != edge.from && vertex != edge.to && s2.DistanceFromSegment(vertex, edge.from, edge.to) < overlayEpsilon {
					splits = append(splits, vertex)
				}
			}
		}
		sort.Slice(splits, func(i, j int) bool { return edge.from.Distance(splits[i]) < edge.from.Distance(splits[j]) })
		current := edge.from
		for _, split := range splits {
			if split.Distance(current) < overlayEpsilon || split.Distance(edge.to) < overlayEpsilon {
				continue
			}
			pieces = append(pieces, overlayEdge{current, split})
			current = split
		}
		pieces = append(pieces, overlayEdge{current, edge.to})
	}
	return pieces
}

func onBoundary(point s2.Point, edges []overlayEdge) bool {
	for _, edge := range edges {
		if s2.DistanceFromSegment(point, edge.from, edge.to) < overlayEpsilon {
			return true
		}
	}
	return false
}

func sameEdge(a, b overlayEdge) bool {
	return a.from.Distance(b.from) < overlayEpsilon && a.to.Distance(b.to) < overlayEpsilon
}

// overlay returns the loops of A ∩ B (union = false) or A ∪ B.
func overlay(a, b *s2.Polygon, union bool) ([]*s2.Loop, bool) {
	loops, _, ok := overlayWithContacts(a, b, union)
	return loops, ok
}

// overlayWithContacts also returns the boundary pieces A and B share with
// their interiors on opposite sides, where the polygons only touch.
func overlayWithContacts(a, b *s2.Polygon, union bool) ([]*s2.Loop, []overlayEdge, bool) {
	edgesA, edgesB := boundaryEdges(a), boundaryEdges(b)
	if len(edgesA)*len(edgesB) > maxOverlayEdges*16 {
		return nil, nil, false
	}
	piecesA, piecesB := splitEdges(edgesA, edgesB), splitEdges(edgesB, edgesA)
	var kept, contacts []overlayEdge
	keep := func(pieces []overlayEdge, other *s2.Polygon, otherEdges, otherPieces []overlayEdge, first bool) {
		for _, piece := range pieces {
			middle := s2.Interpolate(0.5, piece.from, piece.to)
			if onBoundary(middle, otherEdges) {
				// A shared piece is kept once, from A, when both polygons'
				// interiors lie on the same side (the same direction).
				if !first {
					continue
				}
				same := false
				for _, candidate := range otherPieces {
					same = same || sameEdge(piece, candidate)
				}
				if same {
					kept = append(kept, piece)
				} else {
					contacts = append(contacts, piece)
				}
				continue
			}
			if other.ContainsPoint(middle) != union {
				kept = append(kept, piece)
			}
		}
	}
	keep(piecesA, b, edgesB, piecesB, true)
	keep(piecesB, a, edgesA, piecesA, false)
	if len(kept) > maxOverlayEdges {
		return nil, nil, false
	}
	return chainLoops(kept), contacts, true
}

// chainLoops links directed edges end to start into closed loops.
func chainLoops(edges []overlayEdge) []*s2.Loop {
	type key [3]float64
	pointKey := func(p s2.Point) key { return key{p.X, p.Y, p.Z} }
	outgoing := map[key][]int{}
	for i, edge := range edges {
		outgoing[pointKey(edge.from)] = append(outgoing[pointKey(edge.from)], i)
	}
	used := make([]bool, len(edges))
	var loops []*s2.Loop
	for start := range edges {
		if used[start] {
			continue
		}
		var vertices []s2.Point
		current := start
		for steps := 0; steps <= len(edges); steps++ {
			used[current] = true
			vertices = append(vertices, edges[current].from)
			next := -1
			for _, candidate := range outgoing[pointKey(edges[current].to)] {
				if !used[candidate] {
					next = candidate
					break
				}
			}
			if edges[current].to == edges[start].from || next < 0 {
				break
			}
			current = next
		}
		if len(vertices) >= 3 && edges[current].to == edges[start].from {
			loop := s2.LoopFromPoints(vertices)
			if loop.Validate() == nil {
				loops = append(loops, loop)
			}
		}
	}
	return loops
}

// polygonFromLoops assembles result loops, ordered with holes after their
// shells, as a polygon.
func polygonFromLoops(loops []*s2.Loop) *s2.Polygon {
	if len(loops) == 0 {
		return s2.PolygonFromLoops(nil)
	}
	// PolygonFromOrientedLoops keeps each loop's orientation (holes run
	// clockwise), which the overlay preserves.
	return s2.PolygonFromOrientedLoops(loops)
}

// inputCoordinates maps S2 points of input vertices to their coordinates,
// so output vertices that are input vertices keep their exact values.
type inputCoordinates map[s2.Point]geoCoordinate

func (known inputCoordinates) of(p s2.Point) geoCoordinate {
	if c, ok := known[p]; ok {
		return c
	}
	return geoCoordinateOf(p)
}

func collectCoordinates(known inputCoordinates, values ...any) {
	for _, value := range values {
		geometry, ok := parseGeometry(value)
		if !ok {
			continue
		}
		for _, polygon := range geometry.polygons {
			for _, ring := range polygon {
				for _, c := range ring {
					known[c.point()] = c
				}
			}
		}
		for _, line := range geometry.lines {
			for _, c := range line {
				known[c.point()] = c
			}
		}
	}
}

// geoPolygonJSON writes a polygon as a GeoJSON Polygon or MultiPolygon.
func geoPolygonJSON(polygon *s2.Polygon, known ...inputCoordinates) (any, error) {
	rings := polygonRings(polygon, known...)
	switch len(rings) {
	case 0:
		return emptyCollection()
	case 1:
		return geoGeometryJSON("Polygon", rings[0])
	}
	return geoGeometryJSON("MultiPolygon", rings)
}

func polygonRings(polygon *s2.Polygon, known ...inputCoordinates) [][][]geoCoordinate {
	lookup := inputCoordinates{}
	if len(known) > 0 {
		lookup = known[0]
	}
	var polygons [][][]geoCoordinate
	for _, loop := range polygon.Loops() {
		vertices := loop.Vertices()
		ring := make([]geoCoordinate, 0, len(vertices)+1)
		for _, vertex := range vertices {
			ring = append(ring, lookup.of(vertex))
		}
		ring = append(ring, ring[0])
		if !loop.IsHole() || len(polygons) == 0 {
			polygons = append(polygons, [][]geoCoordinate{ring})
		} else {
			polygons[len(polygons)-1] = append(polygons[len(polygons)-1], ring)
		}
	}
	return polygons
}

func emptyCollection() (any, error) {
	return `{"type":"GeometryCollection","geometries":[]}`, nil
}

// memberPolygons returns each GeoJSON polygon of a value as an S2 polygon.
func memberPolygons(value any) ([]*s2.Polygon, bool) {
	_, geometry, ok := geoPolygon(value)
	if !ok {
		return nil, false
	}
	var members []*s2.Polygon
	for _, rings := range geometry.polygons {
		var loops []*s2.Loop
		for _, ring := range rings {
			loop, ok := geoLoop(ring)
			if !ok {
				return nil, false
			}
			loops = append(loops, loop)
		}
		members = append(members, s2.PolygonFromLoops(loops))
	}
	return members, true
}

// unionPolygons folds polygons into their union.
func unionPolygons(members []*s2.Polygon) (*s2.Polygon, bool) {
	if len(members) == 0 {
		return s2.PolygonFromLoops(nil), true
	}
	result := members[0]
	for _, member := range members[1:] {
		if result.IsEmpty() {
			result = member
			continue
		}
		// Polygons that only share edges count as disjoint to S2, so the
		// overlay always runs to merge their shared edges.
		loops, ok := overlay(result, member, true)
		if !ok {
			return nil, false
		}
		result = polygonFromLoops(loops)
	}
	return result, true
}

// geoUnionPolygon is a polygon value's union of its members: a MultiPolygon's
// overlapping polygons measure as their union, as Kusto does.
func geoUnionPolygon(value any) (*s2.Polygon, bool) {
	members, ok := memberPolygons(value)
	if !ok {
		return nil, false
	}
	if len(members) == 1 {
		return members[0], true
	}
	return unionPolygons(members)
}

func kqlGeoIntersection2Polygons(first, second any) (result any, err error) {
	defer recoverGeometry(&result, &err)
	a, okA := geoUnionPolygon(first)
	b, okB := geoUnionPolygon(second)
	if !okA || !okB {
		return nil, nil
	}
	known := inputCoordinates{}
	collectCoordinates(known, first, second)
	loops, contacts, ok := overlayWithContacts(a, b, false)
	if !ok {
		return nil, nil
	}
	shape := polygonFromLoops(loops)
	// Shared edges with the polygons on opposite sides are lines of contact.
	var segments [][]geoCoordinate
	for _, edge := range contacts {
		segments = append(segments, []geoCoordinate{known.of(edge.from), known.of(edge.to)})
	}
	lines := mergeSegments(segments)
	// Vertices where the polygons touch outside the shared area are points
	// of the intersection.
	var touches []geoCoordinate
	boundaryA, boundaryB, boundaryResult := boundaryEdges(a), boundaryEdges(b), boundaryEdges(shape)
	addTouch := func(p s2.Point) {
		if shape.ContainsPoint(p) || onBoundary(p, boundaryResult) {
			return
		}
		for _, edge := range contacts {
			if s2.DistanceFromSegment(p, edge.from, edge.to) < overlayEpsilon {
				return
			}
		}
		c := known.of(p)
		for _, existing := range touches {
			if existing == c {
				return
			}
		}
		touches = append(touches, c)
	}
	for _, edge := range boundaryA {
		if onBoundary(edge.from, boundaryB) {
			addTouch(edge.from)
		}
	}
	for _, edge := range boundaryB {
		if onBoundary(edge.from, boundaryA) {
			addTouch(edge.from)
		}
	}
	rings := polygonRings(shape, known)
	var geometries []json.RawMessage
	add := func(kind string, coordinates any) {
		if encoded, _ := geoGeometryJSON(kind, coordinates); encoded != nil {
			geometries = append(geometries, json.RawMessage(encoded.(string)))
		}
	}
	switch {
	case len(touches) == 1:
		add("Point", touches[0])
	case len(touches) > 1:
		add("MultiPoint", touches)
	}
	switch {
	case len(lines) == 1:
		add("LineString", lines[0])
	case len(lines) > 1:
		add("MultiLineString", lines)
	}
	switch {
	case len(rings) == 1:
		add("Polygon", rings[0])
	case len(rings) > 1:
		add("MultiPolygon", rings)
	}
	switch len(geometries) {
	case 0:
		return emptyCollection()
	case 1:
		return string(geometries[0]), nil
	}
	encoded, err := json.Marshal(map[string]any{"type": "GeometryCollection", "geometries": geometries})
	return string(encoded), err
}

// geoArrayElements decodes a dynamic array of geometries.
func geoArrayElements(value any) ([]any, bool) {
	text, ok := textArgument(value)
	if !ok || sqliteNull(value) || len(text) > maxReplacementBytes {
		return nil, false
	}
	var raw []json.RawMessage
	if json.Unmarshal([]byte(text), &raw) != nil || len(raw) > 4096 {
		return nil, false
	}
	elements := make([]any, len(raw))
	for i, element := range raw {
		elements[i] = string(element)
	}
	return elements, true
}

func kqlGeoUnionPolygonsArray(value any) (result any, err error) {
	defer recoverGeometry(&result, &err)
	elements, ok := geoArrayElements(value)
	if !ok {
		return nil, nil
	}
	var members []*s2.Polygon
	for _, element := range elements {
		polygons, ok := memberPolygons(element)
		if !ok {
			return nil, nil
		}
		members = append(members, polygons...)
	}
	union, ok := unionPolygons(members)
	if !ok || union.IsEmpty() {
		return nil, nil
	}
	known := inputCoordinates{}
	collectCoordinates(known, elements...)
	return geoPolygonJSON(union, known)
}

func kqlGeoSimplifyPolygonsArray(value, toleranceValue any) (result any, err error) {
	defer recoverGeometry(&result, &err)
	union, err := kqlGeoUnionPolygonsArray(value)
	if err != nil || union == nil {
		return nil, err
	}
	return kqlGeoPolygonSimplify(union, toleranceValue)
}

// kqlGeoIntersection2Lines returns the points where two lines cross, or
// the segments they share.
func kqlGeoIntersection2Lines(first, second any) (result any, err error) {
	defer recoverGeometry(&result, &err)
	a, _, okA := geoLines(first)
	b, _, okB := geoLines(second)
	if !okA || !okB {
		return nil, nil
	}
	known := inputCoordinates{}
	collectCoordinates(known, first, second)
	var points []geoCoordinate
	var segments [][]geoCoordinate
	addPoint := func(p s2.Point) {
		for _, existing := range points {
			if existing.point().Distance(p) < overlayEpsilon {
				return
			}
		}
		points = append(points, known.of(p))
	}
	for _, lineA := range a {
		for i := 1; i < len(lineA); i++ {
			a0, a1 := lineA[i-1], lineA[i]
			for _, lineB := range b {
				for j := 1; j < len(lineB); j++ {
					b0, b1 := lineB[j-1], lineB[j]
					// Collinear overlap: both ends of one edge on the other.
					if s2.DistanceFromSegment(b0, a0, a1) < overlayEpsilon && s2.DistanceFromSegment(b1, a0, a1) < overlayEpsilon {
						segments = append(segments, []geoCoordinate{known.of(b0), known.of(b1)})
						continue
					}
					if s2.DistanceFromSegment(a0, b0, b1) < overlayEpsilon && s2.DistanceFromSegment(a1, b0, b1) < overlayEpsilon {
						segments = append(segments, []geoCoordinate{known.of(a0), known.of(a1)})
						continue
					}
					switch s2.CrossingSign(a0, a1, b0, b1) {
					case s2.Cross:
						addPoint(s2.Intersection(a0, a1, b0, b1))
					case s2.MaybeCross:
						for _, vertex := range []s2.Point{a0, a1} {
							if vertex == b0 || vertex == b1 {
								addPoint(vertex)
							}
						}
					}
				}
			}
		}
	}
	segments = mergeSegments(segments)
	// Points on shared segments are part of them.
	var loose []geoCoordinate
	for _, point := range points {
		covered := false
		for _, segment := range segments {
			for k := 1; k < len(segment); k++ {
				covered = covered || s2.DistanceFromSegment(point.point(), segment[k-1].point(), segment[k].point()) < overlayEpsilon
			}
		}
		if !covered {
			loose = append(loose, point)
		}
	}
	return lineIntersectionJSON(loose, segments)
}

// mergeSegments joins consecutive shared segments into paths.
func mergeSegments(segments [][]geoCoordinate) [][]geoCoordinate {
	var paths [][]geoCoordinate
	for _, segment := range segments {
		if n := len(paths); n > 0 && paths[n-1][len(paths[n-1])-1] == segment[0] {
			paths[n-1] = append(paths[n-1], segment[1:]...)
			continue
		}
		duplicate := false
		for _, path := range paths {
			for k := 1; k < len(path); k++ {
				duplicate = duplicate || (path[k-1] == segment[0] && path[k] == segment[1])
			}
		}
		if !duplicate {
			paths = append(paths, append([]geoCoordinate{}, segment...))
		}
	}
	return paths
}

func lineIntersectionJSON(points []geoCoordinate, paths [][]geoCoordinate) (any, error) {
	var geometries []json.RawMessage
	addGeometry := func(kind string, coordinates any) {
		encoded, _ := geoGeometryJSON(kind, coordinates)
		if text, ok := encoded.(string); ok {
			geometries = append(geometries, json.RawMessage(text))
		}
	}
	switch {
	case len(paths) == 1:
		addGeometry("LineString", paths[0])
	case len(paths) > 1:
		addGeometry("MultiLineString", paths)
	}
	switch {
	case len(points) == 1:
		addGeometry("Point", points[0])
	case len(points) > 1:
		addGeometry("MultiPoint", points)
	}
	switch len(geometries) {
	case 0:
		return emptyCollection()
	case 1:
		return string(geometries[0]), nil
	}
	encoded, err := json.Marshal(map[string]any{"type": "GeometryCollection", "geometries": geometries})
	return string(encoded), err
}

// kqlGeoIntersectionLineWithPolygon clips a line to a polygon: each edge is
// split where it crosses the boundary and pieces inside are kept.
func kqlGeoIntersectionLineWithPolygon(lineValue, polygonValue any) (result any, err error) {
	defer recoverGeometry(&result, &err)
	lines, _, okLines := geoLines(lineValue)
	polygon, okPolygon := geoUnionPolygon(polygonValue)
	if !okLines || !okPolygon {
		return nil, nil
	}
	boundary := boundaryEdges(polygon)
	var paths [][]geoCoordinate
	var current []geoCoordinate
	flush := func() {
		if len(current) >= 2 {
			paths = append(paths, current)
		}
		current = nil
	}
	for _, line := range lines {
		edges := make([]overlayEdge, 0, len(line))
		for i := 1; i < len(line); i++ {
			edges = append(edges, overlayEdge{line[i-1], line[i]})
		}
		for _, piece := range splitEdges(edges, boundary) {
			middle := s2.Interpolate(0.5, piece.from, piece.to)
			if polygon.ContainsPoint(middle) || onBoundary(middle, boundary) {
				from := geoCoordinateOf(piece.from)
				if len(current) == 0 || current[len(current)-1] != from {
					flush()
					current = []geoCoordinate{from}
				}
				current = append(current, geoCoordinateOf(piece.to))
			} else {
				flush()
			}
		}
		flush()
	}
	switch len(paths) {
	case 0:
		return emptyCollection()
	case 1:
		return geoGeometryJSON("LineString", paths[0])
	}
	return geoGeometryJSON("MultiLineString", paths)
}

// kqlGeoUnionLinesArray combines lines into one MultiLineString (or
// LineString), dropping repeated segments.
func kqlGeoUnionLinesArray(value any) (result any, err error) {
	defer recoverGeometry(&result, &err)
	elements, ok := geoArrayElements(value)
	if !ok {
		return nil, nil
	}
	var segments [][]geoCoordinate
	for _, element := range elements {
		_, paths, ok := geoLines(element)
		if !ok {
			return nil, nil
		}
		for _, path := range paths {
			for k := 1; k < len(path); k++ {
				segments = append(segments, []geoCoordinate{path[k-1], path[k]})
			}
		}
	}
	paths := mergeSegments(segments)
	switch len(paths) {
	case 0:
		return nil, nil
	case 1:
		return geoGeometryJSON("LineString", paths[0])
	}
	return geoGeometryJSON("MultiLineString", paths)
}

// kqlGeoPointBuffer approximates the circle of radius meters around a point
// as a polygon whose vertices lie within tolerance meters of the circle.
func kqlGeoPointBuffer(lngValue, latValue, radiusValue, toleranceValue any) (result any, err error) {
	defer recoverGeometry(&result, &err)
	center, ok := geoCoordinateArgs(lngValue, latValue)
	radius, okRadius := geoNumber(radiusValue)
	tolerance, okTolerance := geoTolerance(toleranceValue, 0.00001, math.MaxFloat64)
	if !ok || !okRadius || !okTolerance || radius <= 0 || radius > 0.5*math.Pi*geoEarthRadius {
		return nil, nil
	}
	angle := radius / geoEarthRadius
	// The chord sagitta r(1-cos(θ/2)) within tolerance sets the vertex count.
	step := 2 * math.Acos(math.Max(-1, 1-tolerance/radius))
	count := int(math.Ceil(2 * math.Pi / step))
	count = min(max(count, 8), 1<<14)
	vertices := make([]s2.Point, count)
	north := s2.PointFromLatLng(s2.LatLngFromDegrees(90, 0))
	axis := center.point()
	// A point at the radius toward north (or another reference at the pole).
	reference := s2.Point{Vector: axis.Cross(north.Vector).Normalize()}
	if reference.Norm() == 0 || math.IsNaN(reference.X) {
		reference = s2.Point{Vector: axis.Ortho()}
	}
	for i := range vertices {
		theta := 2 * math.Pi * float64(i) / float64(count)
		direction := s2.Point{Vector: reference.Mul(math.Cos(theta)).Add(axis.Cross(reference.Vector).Mul(math.Sin(theta)))}
		vertices[i] = s2.Point{Vector: axis.Mul(math.Cos(angle)).Add(direction.Mul(math.Sin(angle))).Normalize()}
	}
	loop := s2.LoopFromPoints(vertices)
	loop.Normalize()
	return geoPolygonJSON(s2.PolygonFromLoops([]*s2.Loop{loop}))
}
