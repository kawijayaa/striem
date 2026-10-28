package database

import (
	"encoding/json"

	"github.com/golang/geo/s1"
	"github.com/golang/geo/s2"
	"github.com/uber/h3-go/v4"
)

// Kusto's limits for H3 results: rings up to distance 142, children at most
// five levels below a cell, and 61680 cells for a polygon.
const (
	maxH3RingDistance = 142
	maxH3ChildLevels  = 5
	maxH3PolygonCells = 61680
)

func h3CellArgument(value any) (h3.Cell, bool) {
	text, ok := textArgument(value)
	if !ok || sqliteNull(value) || len(text) == 0 || len(text) > 16 {
		return 0, false
	}
	for _, char := range text {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f' || char >= 'A' && char <= 'F') {
			return 0, false
		}
	}
	cell := h3.CellFromString(text)
	return cell, cell.IsValid()
}

func h3Tokens(cells []h3.Cell) (any, error) {
	tokens := make([]string, 0, len(cells))
	for _, cell := range cells {
		if cell != 0 {
			tokens = append(tokens, cell.String())
		}
	}
	encoded, err := json.Marshal(tokens)
	return string(encoded), err
}

func kqlGeoPointToH3Cell(lngValue, latValue, resolutionValue any) any {
	c, ok := geoCoordinateArgs(lngValue, latValue)
	resolution, isInt := resolutionValue.(int64)
	if !ok || !isInt || resolution < 0 || resolution > 15 {
		return nil
	}
	cell, err := h3.LatLngToCell(h3.NewLatLng(c.lat, c.lng), int(resolution))
	if err != nil {
		return nil
	}
	return cell.String()
}

func kqlGeoH3CellLevel(value any) any {
	cell, ok := h3CellArgument(value)
	if !ok {
		return nil
	}
	return int64(cell.Resolution())
}

// kqlGeoH3CellParent defaults to the immediate parent (resolution passed as
// NULL); an invalid or finer resolution yields null.
func kqlGeoH3CellParent(value, resolutionValue any) any {
	cell, ok := h3CellArgument(value)
	if !ok {
		return nil
	}
	resolution := int64(cell.Resolution() - 1)
	if resolutionValue != nil && !sqliteNull(resolutionValue) {
		number, isInt := resolutionValue.(int64)
		if !isInt {
			return nil
		}
		resolution = number
	}
	if resolution < 0 || resolution > 14 || resolution > int64(cell.Resolution()) {
		return nil
	}
	parent, err := cell.Parent(int(resolution))
	if err != nil {
		return nil
	}
	return parent.String()
}

func kqlGeoH3CellChildren(value, resolutionValue any) (any, error) {
	cell, ok := h3CellArgument(value)
	if !ok {
		return nil, nil
	}
	resolution := int64(cell.Resolution() + 1)
	if resolutionValue != nil && !sqliteNull(resolutionValue) {
		number, isInt := resolutionValue.(int64)
		if !isInt {
			return nil, nil
		}
		resolution = number
	}
	if resolution < 1 || resolution > 15 || resolution < int64(cell.Resolution()) || resolution-int64(cell.Resolution()) > maxH3ChildLevels {
		return nil, nil
	}
	children, err := cell.Children(int(resolution))
	if err != nil {
		return nil, nil
	}
	return h3Tokens(children)
}

func kqlGeoH3CellNeighbors(value any) (any, error) {
	cell, ok := h3CellArgument(value)
	if !ok {
		return nil, nil
	}
	// Kusto lists neighbors in grid-disk order, without the cell itself.
	disk, err := cell.GridDisk(1)
	if err != nil {
		return nil, nil
	}
	neighbors := make([]h3.Cell, 0, len(disk))
	for _, neighbor := range disk {
		if neighbor != cell {
			neighbors = append(neighbors, neighbor)
		}
	}
	return h3Tokens(neighbors)
}

func kqlGeoH3CellRings(value, distanceValue any) (any, error) {
	cell, ok := h3CellArgument(value)
	distance, isInt := distanceValue.(int64)
	if !ok || !isInt || distance < 0 || distance > maxH3RingDistance {
		return nil, nil
	}
	rings, err := cell.GridDiskDistances(int(distance))
	if err != nil {
		return nil, nil
	}
	result := make([][]string, len(rings))
	for i, ring := range rings {
		result[i] = []string{}
		for _, member := range ring {
			if member != 0 {
				result[i] = append(result[i], member.String())
			}
		}
	}
	encoded, err := json.Marshal(result)
	return string(encoded), err
}

func kqlGeoH3CellToCentralPoint(value any) (any, error) {
	cell, ok := h3CellArgument(value)
	if !ok {
		return nil, nil
	}
	center, err := cell.LatLng()
	if err != nil {
		return nil, nil
	}
	return geoPointJSON(geoCoordinate{lng: center.Lng, lat: center.Lat})
}

func kqlGeoH3CellToPolygon(value any) (any, error) {
	cell, ok := h3CellArgument(value)
	if !ok {
		return nil, nil
	}
	boundary, err := cell.Boundary()
	if err != nil || len(boundary) == 0 {
		return nil, nil
	}
	ring := make([]geoCoordinate, 0, len(boundary)+1)
	for _, vertex := range boundary {
		ring = append(ring, geoCoordinate{lng: vertex.Lng, lat: vertex.Lat})
	}
	ring = append(ring, ring[0])
	return geoGeometryJSON("Polygon", [][]geoCoordinate{ring})
}

// kqlGeoPolygonToH3Cells returns the cells whose centers lie in the polygon
// (planar edges, as H3 uses), or within radius meters of it.
func kqlGeoPolygonToH3Cells(polygonValue, resolutionValue, radiusValue any) (any, error) {
	polygon, geometry, ok := geoPolygon(polygonValue)
	resolution, isInt := resolutionValue.(int64)
	radius, okRadius := geoNumber(radiusValue)
	if !ok || !isInt || !okRadius || resolution < 0 || resolution > 15 || radius < 0 {
		return nil, nil
	}
	cellArea, err := h3.HexagonAreaAvgM2(int(resolution))
	if err != nil {
		return nil, nil
	}
	buffer := radius / geoEarthRadius
	estimate := (polygon.Area() + buffer*buffer*3.15 + buffer*perimeterAngle(polygon)) * geoEarthRadius * geoEarthRadius / cellArea
	if estimate > 2*maxH3PolygonCells {
		return nil, nil
	}
	seen := map[h3.Cell]bool{}
	var cells []h3.Cell
	for _, rings := range geometry.polygons {
		shape := h3.GeoPolygon{GeoLoop: h3Loop(rings[0])}
		for _, hole := range rings[1:] {
			shape.Holes = append(shape.Holes, h3Loop(hole))
		}
		found, err := h3.PolygonToCells(shape, int(resolution))
		if err != nil {
			return nil, nil
		}
		for _, cell := range found {
			if !seen[cell] {
				seen[cell] = true
				cells = append(cells, cell)
			}
		}
	}
	if radius > 0 {
		if cells, ok = bufferH3Cells(polygon, geometry, cells, seen, int(resolution), s1.Angle(buffer)); !ok {
			return nil, nil
		}
	}
	if len(cells) > maxH3PolygonCells {
		return nil, nil
	}
	return h3Tokens(cells)
}

func h3Loop(ring []geoCoordinate) h3.GeoLoop {
	loop := make(h3.GeoLoop, 0, len(ring))
	for _, c := range ring[:len(ring)-1] {
		loop = append(loop, h3.NewLatLng(c.lat, c.lng))
	}
	return loop
}

// bufferH3Cells adds cells whose centers lie within radius of the polygon,
// walking outward from the cells found so far (or from the vertices).
func bufferH3Cells(polygon *s2.Polygon, geometry geoGeometry, cells []h3.Cell, seen map[h3.Cell]bool, resolution int, radius s1.Angle) ([]h3.Cell, bool) {
	queue := append([]h3.Cell{}, cells...)
	for _, rings := range geometry.polygons {
		for _, c := range rings[0] {
			cell, err := h3.LatLngToCell(h3.NewLatLng(c.lat, c.lng), resolution)
			if err == nil && !seen[cell] {
				queue = append(queue, cell)
			}
		}
	}
	boundary := polygonBoundary(polygon)
	within := func(cell h3.Cell) bool {
		center, err := cell.LatLng()
		if err != nil {
			return false
		}
		point := geoCoordinate{lng: center.Lng, lat: center.Lat}.point()
		if polygon.ContainsPoint(point) {
			return true
		}
		_, distance := closestOnLines(boundary, point)
		return distance <= radius
	}
	for len(queue) > 0 {
		cell := queue[0]
		queue = queue[1:]
		if !seen[cell] {
			seen[cell] = true
			if !within(cell) {
				continue
			}
			cells = append(cells, cell)
		}
		ring, err := cell.GridRing(1)
		if err != nil {
			continue
		}
		for _, neighbor := range ring {
			if neighbor == 0 || seen[neighbor] {
				continue
			}
			seen[neighbor] = true
			if within(neighbor) {
				cells = append(cells, neighbor)
				if len(cells) > maxH3PolygonCells {
					return nil, false
				}
				queue = append(queue, neighbor)
			}
		}
	}
	return cells, true
}
