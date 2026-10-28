package kql

import (
	"fmt"
	"testing"
	"time"
)

// Microsoft's geo_* examples, with invalid coordinates, rings and options.
func TestGeoFunctions(t *testing.T) {
	db := featureDatabase(t)
	park := `dynamic({"type":"Polygon","coordinates":[[[-73.9495,40.7969],[-73.95807266235352,40.80068603561921],[-73.98201942443848,40.76825672305777],[-73.97317886352539,40.76455136505513],[-73.9495,40.7969]]]})`
	vegas := `dynamic({ "type":"LineString","coordinates":[[-115.115385,36.229195],[-115.136995,36.200366],[-115.140252,36.192470],[-115.143558,36.188523],[-115.144076,36.181954],[-115.154662,36.174483],[-115.166431,36.176388],[-115.183289,36.175007],[-115.192612,36.176736],[-115.202485,36.173439],[-115.225355,36.174365]]})`
	for _, tc := range []struct{ query, want string }{
		{`print d=geo_distance_2points(-122.407628, 47.578557, -118.275287, 34.019056), s=round(geo_distance_2points(-122.407628, 47.578557, -118.275287, 34.019056, true)), bad=geo_distance_2points(300,1,1,1)`, `[[1.5467543519738093e+06 1.545044e+06 <nil>]]`},
		{`print a=geo_azimuth(5, 10, 10, -40), same=isnull(geo_azimuth(5, 10, 5, 10)), angle=geo_angle(0, 10, 0,5, 3,-10), degenerate=isnull(geo_angle(0, 10, 0, 10, 3, -10))`, `[[3.054599397964492 1 2.944938434068817 1]]`},
		{`print a=geo_point_in_circle(-122.143564, 47.535677, -122.100896, 47.527351, 3500), b=geo_point_in_circle(-122.137575, 47.630683, -122.100896, 47.527351, 3500), c=geo_point_in_circle(200, 1, 1, 1, 1), d=geo_point_in_circle(1, 1, 1, 1, -1)`, `[[1 0 <nil> <nil>]]`},
		{`print inside=geo_point_in_polygon(-73.965, 40.785, ` + park + `), outside=geo_point_in_polygon(-73.9839, 40.7705, ` + park + `), bad=geo_point_in_polygon(1,1,dynamic({"type": "Polygon","coordinates": [[[0,0],[10,10],[10,10],[0,0]]]})), open=geo_point_in_polygon(1,1,dynamic({"type": "Polygon","coordinates": [[[0,0],[10,10],[10,1]]]}))`, `[[1 0 <nil> <nil>]]`},
		{`print a=round(geo_polygon_area(` + park + `), 3), p=round(geo_polygon_perimeter(` + park + `), 3), c=geo_polygon_centroid(` + park + `).coordinates[0] between (-73.9658 .. -73.9657)`, `[[3.475207283e+06 9930.301 1]]`},
		{`print d=round(geo_distance_point_to_line(-115.199625, 36.210419, ` + vegas + `), 6), c=geo_closest_point_on_line(-115.199625, 36.210419, ` + vegas + `), bad=geo_distance_point_to_line(1,1, dynamic({"type":"LineString"}))`, `[[3797.888873 {"type":"Point","coordinates":[-115.192612,36.176736]} <nil>]]`},
		{`print d=round(geo_distance_point_to_polygon(-73.9839, 40.7705, ` + park + `), 6), in=geo_distance_point_to_polygon(-73.965, 40.785, ` + park + `)`, `[[259.940756 0]]`},
		{`let line = dynamic({"type":"LineString","coordinates":[[-73.95807266235352,40.800426144169315],[-73.94966125488281,40.79691751000055],[-73.97317886352539,40.764486356930334]]}); print l=round(geo_line_length(line), 6), m=round(geo_line_length(dynamic({"type":"MultiLineString","coordinates":[[[-73.95798683166502,40.800556090021466],[-73.98193359375,40.76819171855746]],[[-73.94940376281738,40.79691751000055],[-73.97317886352539,40.76435634049001]]]})), 6), bad=isnull(geo_line_length(dynamic({"type":"LineString","coordinates":[[1, 1]]})))`, `[[4922.48017 8262.243398 1]]`},
		{`let line = dynamic({"type":"LineString","coordinates":[[-73.95796, 40.80042], [-73.97317, 40.764486]]}); print f=round(geo_line_locate_point(line, -73.965, 40.792), 9), p=round(todouble(geo_line_interpolate_point(line, 0.25).coordinates[0]), 9), bad=isnull(geo_line_interpolate_point(line, 2))`, `[[0.255601351 -73.961764043 1]]`},
		{`print a=tostring(geo_line_densify(dynamic({"type":"LineString","coordinates":[[50, 50], [51, 51]]}))), b=geo_line_densify(dynamic({"type":"LineString","coordinates":[[1,1],[2,2]]}), 0), c=tostring(geo_polygon_densify(dynamic({"type":"Polygon","coordinates":[[[10,10],[11,10],[11,11],[10,11],[10,10]]]})))`,
			`[[{"type":"LineString","coordinates":[[50,50],[50.125,50.125],[50.25,50.25],[50.375,50.375],[50.5,50.5],[50.625,50.625],[50.75,50.75],[50.875,50.875],[51,51]]} <nil> {"type":"Polygon","coordinates":[[[10,10],[10.25,10],[10.5,10],[10.75,10],[11,10],[11,11],[10.75,11],[10.5,11],[10.25,11],[10,11],[10,10]]]}]]`},
		{`print a=geo_line_simplify(dynamic({"type":"LineString","coordinates":[[-73.97033169865608,40.789063020152824],[-73.97039607167244,40.78897975920816],[-73.9704617857933,40.78888837512432],[-73.97079303860664,40.788434498977836]]})), b=isnull(geo_line_simplify(dynamic({"type":"LineString","coordinates":[[1.1, 1.1],[1.2,1.2]]}), 100000)), c=isnull(geo_line_simplify(dynamic({"type":"LineString","coordinates":[[1, 1],[2,2]]}), -1))`,
			`[[{"type":"LineString","coordinates":[[-73.97033169865608,40.789063020152824],[-73.97079303860664,40.788434498977836]]} 1 1]]`},
		{`print a=geo_intersects_2lines(dynamic({"type":"LineString","coordinates":[[-73.978929,40.785155],[-73.980903,40.782621]]}), dynamic({"type":"LineString","coordinates":[[-73.985195,40.788275],[-73.974552,40.779761]]})), b=geo_intersects_2lines(dynamic({"type":"LineString","coordinates":[[1,1],[2,2]]}), dynamic({"type":"LineString","coordinates":[[3,3],[4,4]]})), c=geo_intersects_line_with_polygon(dynamic({"type":"LineString","coordinates":[[-73.985195,40.788275],[-73.974552,40.779761]]}), dynamic({"type":"Polygon","coordinates":[[[-73.9712905883789,40.78580561168767],[-73.98004531860352,40.775276834803655],[-73.97000312805176,40.77852663535664],[-73.9712905883789,40.78580561168767]]]})), d=geo_intersects_2polygons(dynamic({"type":"Polygon","coordinates":[[[2,45],[0,45],[1,44],[2,45]]]}), dynamic({"type":"Polygon","coordinates":[[[3,44],[3,45],[2,43],[3,44]]]}))`, `[[1 0 1 0]]`},
		{`print h=geo_point_to_geohash(-80.195829, 25.802215, 8), d=geo_point_to_geohash(-122.303404, 47.570482), n=geo_point_to_geohash(1,1,int(null)), bad=geo_point_to_geohash(200,1,8), p=geo_geohash_to_polygon("dr5ru"), c=geo_geohash_to_central_point("sunny"), x=geo_geohash_to_central_point("a")`,
			`[[dhwfz15h c23n8 <nil> <nil> {"type":"Polygon","coordinates":[[[-74.00390625,40.7373046875],[-73.9599609375,40.7373046875],[-73.9599609375,40.78125],[-74.00390625,40.78125],[-74.00390625,40.7373046875]]]} {"type":"Point","coordinates":[42.47314453125,23.70849609375]} <nil>]]`},
		{`print n=array_sort_asc(geo_geohash_neighbors('sunny')), polar=array_length(geo_geohash_neighbors('b')), bad=isnull(geo_geohash_neighbors('a'))`, `[[["sunnt","sunnv","sunnw","sunnx","sunnz","sunpj","sunpn","sunpp"] 5 1]]`},
		{`print s=geo_point_to_s2cell(-80.195829, 25.802215, 8), d=geo_point_to_s2cell(10.1234, 53), bad=geo_point_to_s2cell(1,1,35), n=array_sort_asc(geo_s2cell_neighbors('89c259')), c=isnull(geo_s2cell_to_central_point('zz')), p=array_length(geo_s2cell_to_polygon('89c259').coordinates[0])`,
			`[[88d9b 47b1ccc <nil> ["89c251","89c257","89c25b","89c25d","89c25f","89c2f5","89c2f7","89c2f9"] 1 5]]`},
		{`print a=array_length(geo_polygon_to_s2cells(` + park + `, 15)), b=array_length(geo_polygon_to_s2cells(` + park + `, 15, 500)) > array_length(geo_polygon_to_s2cells(` + park + `, 15)), huge=isnull(geo_polygon_to_s2cells(dynamic({"type":"Polygon","coordinates":[[[0,0],[40,0],[40,40],[0,0]]]}), 20))`, `[[79 1 1]]`},
		{`print p=geo_from_wkt("POINT (1 2)"), l=geo_from_wkt("LINESTRING (1 2, 3 4)"), m=geo_from_wkt("MULTIPOINT ((1 1), 2 2)"), c=geo_from_wkt("GEOMETRYCOLLECTION (POINT (1 1),POLYGON ((10 10, 12 10, 12 12, 10 10)))"), z=geo_from_wkt("POINT Z (1 2 3)"), bad=isnull(geo_from_wkt("LINESTRING")), range=isnull(geo_from_wkt("POINT (500 1)"))`,
			`[[{"type":"Point","coordinates":[1,2]} {"type":"LineString","coordinates":[[1,2],[3,4]]} {"type":"MultiPoint","coordinates":[[1,1],[2,2]]} {"type":"GeometryCollection","geometries":[{"type":"Point","coordinates":[1,1]},{"type":"Polygon","coordinates":[[[10,10],[12,10],[12,12],[10,10]]]}]} {"type":"Point","coordinates":[1,2]} 1 1]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Errorf("%s\n got %s\nwant %s", tc.query, got, tc.want)
		}
	}
	for _, query := range []string{`print geo_distance_2points(1, 2, 3)`, `print geo_point_to_geohash("x", 1)`, `print geo_line_length("x")`, `print geo_point_in_polygon(1, 2, 3)`} {
		if _, err := Compile(query, time.Now()); err == nil {
			t.Fatalf("invalid call accepted: %s", query)
		}
	}
}

// Microsoft's H3 examples: tokens, hierarchy, rings and polygon cells.
func TestGeoH3Functions(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print a=geo_point_to_h3cell(-74.04450446039874, 40.689250859314974, 6), d=geo_point_to_h3cell(-73.989148, 40.743273, 5), bad=geo_point_to_h3cell(300,1,8), res=geo_point_to_h3cell(1,1,16), n=geo_point_to_h3cell(1,1,int(null)), def=geo_h3cell_level(geo_point_to_h3cell(1,1))`, `[[862a1072fffffff 852a1073fffffff <nil> <nil> <nil> 6]]`},
		{`print l=geo_h3cell_level('862a1072fffffff'), bad=isnull(geo_h3cell_level('abc')), p=geo_h3cell_parent('862a1072fffffff'), p1=geo_h3cell_parent('862a1072fffffff', 1), fine=isempty(geo_h3cell_parent('862a1072fffffff', 15)), far=isempty(geo_h3cell_parent('862a1072fffffff', 100))`, `[[6 1 852a1073fffffff 812a3ffffffffff 1 1]]`},
		{`print c=geo_h3cell_children('862a1072fffffff'), n=array_length(geo_h3cell_children('862a1072fffffff', 9)), deep=isnull(geo_h3cell_children('862a1072fffffff', 12)), up=isnull(geo_h3cell_children(geo_point_to_h3cell(1, 1, 9), 8))`,
			`[[["872a10728ffffff","872a10729ffffff","872a1072affffff","872a1072bffffff","872a1072cffffff","872a1072dffffff","872a1072effffff"] 343 1 1]]`},
		{`print n=geo_h3cell_neighbors('862a1072fffffff'), r=geo_h3cell_rings('861f8894fffffff', 1), far=isnull(geo_h3cell_rings('861f8894fffffff', 150)), zero=geo_h3cell_rings('861f8894fffffff', 0)`,
			`[[["862a10727ffffff","862a10707ffffff","862a1070fffffff","862a10777ffffff","862a100dfffffff","862a100d7ffffff"] [["861f8894fffffff"],["861f88947ffffff","861f8895fffffff","861f88867ffffff","861f8d497ffffff","861f8d4b7ffffff","861f8896fffffff"]] 1 [["861f8894fffffff"]]]]`},
		{`print c=geo_h3cell_to_central_point("862a1072fffffff").coordinates[0], p=array_length(geo_h3cell_to_polygon("862a1072fffffff").coordinates[0]), bad=isnull(geo_h3cell_to_polygon("@")), pentagon=array_length(geo_h3cell_to_polygon("8009fffffffffff").coordinates[0])`, `[[-74.01600847979245 7 1 6]]`},
		{`let polygon = dynamic({"type":"Polygon","coordinates":[[[-3.659,40.553],[-3.913,40.409],[-3.729,40.273],[-3.524,40.440],[-3.659,40.553]]]}); print cells = array_sort_asc(geo_polygon_to_h3cells(polygon)), wider=array_length(geo_polygon_to_h3cells(polygon, 6, 2000)) > 13, bad=isnull(geo_polygon_to_h3cells(dynamic({"type":"Polygon","coordinates":[[[0,0],[1,1]]]}))), huge=isnull(geo_polygon_to_h3cells(dynamic({"type":"Polygon","coordinates":[[[0,0],[40,0],[40,40],[0,0]]]}), 12))`,
			`[[["86390ca07ffffff","86390ca27ffffff","86390ca2fffffff","86390ca37ffffff","86390cb07ffffff","86390cb0fffffff","86390cb17ffffff","86390cb1fffffff","86390cb57ffffff","86390cb87ffffff","86390cb8fffffff","86390cba7ffffff","86390cbafffffff"] 1 1 1]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Errorf("%s\n got %s\nwant %s", tc.query, got, tc.want)
		}
	}
}

// Microsoft's intersection, union and buffer examples, plus measurements of
// overlapping MultiPolygons (Kusto measures their union).
func TestGeoOverlay(t *testing.T) {
	db := featureDatabase(t)
	two := `dynamic({"type":"MultiPolygon","coordinates":[[[[-73.9495,40.7969],[-73.95807266235352,40.80068603561921],[-73.98201942443848,40.76825672305777],[-73.97317886352539,40.76455136505513],[-73.9495,40.7969]]],[[[-73.94262313842773,40.775991804565585],[-73.98107528686523,40.791849155467695],[-73.99600982666016,40.77092185281977],[-73.96150588989258,40.75609977566361],[-73.94262313842773,40.775991804565585]]]]})`
	for _, tc := range []struct{ query, want string }{
		{`print a=round(geo_polygon_area(` + two + `)), p=round(geo_polygon_perimeter(` + two + `), 1), c=round(todouble(geo_polygon_centroid(` + two + `).coordinates[0]), 6)`, `[[1.0889972e+07 15943.5 -73.96857]]`},
		{`print i=geo_intersection_2lines(dynamic({"type":"LineString","coordinates":[[1, 1],[2, 2]]}), dynamic({"type":"LineString","coordinates":[[3, 3],[4, 4]]})), j=geo_intersection_2lines(dynamic({"type":"LineString","coordinates":[[0,0],[2,2]]}), dynamic({"type":"LineString","coordinates":[[0,2],[2,0]]})).type, k=geo_intersection_2lines(dynamic({"type":"LineString","coordinates":[[-73.978929,40.785155],[-73.980903,40.782621]]}), dynamic({"type":"LineString","coordinates":[[-73.978929,40.785155],[-73.980903,40.782621]]})), bad=isnull(geo_intersection_2lines(dynamic({"type":"LineString","coordinates":[[1, 1],[2, 2]]}), dynamic({"type":"LineString","coordinates":[[3, 3]]})))`,
			`[[{"type":"GeometryCollection","geometries":[]} Point {"type":"LineString","coordinates":[[-73.978929,40.785155],[-73.980903,40.782621]]} 1]]`},
		{`print touch=geo_intersection_2polygons(dynamic({"type":"Polygon","coordinates":[[[2,45],[0,45],[1,44],[2,45]]]}), dynamic({"type":"Polygon","coordinates":[[[3,44],[2,45],[2,43],[3,44]]]})), none=geo_intersection_2polygons(dynamic({"type":"Polygon","coordinates":[[[2,45],[0,45],[1,44],[2,45]]]}), dynamic({"type":"Polygon","coordinates":[[[3,44],[3,45],[2,43],[3,44]]]})), bad=isnull(geo_intersection_2polygons(dynamic({"type":"Polygon"}), dynamic({"type":"Polygon","coordinates":[[[3,44],[3,45],[2,43],[3,44]]]})))`,
			`[[{"type":"Point","coordinates":[2,45]} {"type":"GeometryCollection","geometries":[]} 1]]`},
		{`print s=geo_intersection_2polygons(dynamic({"type":"Polygon","coordinates":[[[0,0],[2,0],[2,2],[0,2],[0,0]]]}), dynamic({"type":"Polygon","coordinates":[[[1,1],[3,1],[3,3],[1,3],[1,1]]]})) | project t=s.type, a=round(geo_polygon_area(s) / 1e9, 2)`, `[[Polygon 12.36]]`},
		{`print u=geo_union_polygons_array(pack_array(geo_geohash_to_polygon("sunny"), geo_geohash_to_polygon("sunpn"))) | project t=u.type, n=array_length(u.coordinates[0])`, `[[Polygon 7]]`},
		{`print u=geo_union_polygons_array(pack_array(dynamic({"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,0]]]}), dynamic({"type":"Polygon","coordinates":[[[10,10],[11,10],[11,11],[10,10]]]}))) | project t=u.type`, `[[MultiPolygon]]`},
		{`let lineString = dynamic({"type":"LineString","coordinates":[[-110.522, 39.198],[-91.428, 40.880]]}); let polygon = dynamic({"type":"Polygon","coordinates":[[[-90.263,36.738],[-102.041,45.274],[-109.335,36.527],[-90.263,36.738]],[[-100.393,41.705],[-103.139,38.925],[-97.558,39.113],[-100.393,41.705]]]}); print i=geo_intersection_line_with_polygon(lineString, polygon) | project t=i.type, n=array_length(i.coordinates)`, `[[MultiLineString 2]]`},
		{`print u=geo_union_lines_array(pack_array(dynamic({"type":"LineString","coordinates":[[0,0],[1,1]]}), dynamic({"type":"LineString","coordinates":[[1,1],[2,2]]}), dynamic({"type":"LineString","coordinates":[[0,0],[1,1]]})))`, `[[{"type":"LineString","coordinates":[[0,0],[1,1],[2,2]]}]]`},
		{`print b=geo_point_buffer(0, 0, 1000) | project t=b.type, inside=geo_point_in_polygon(0.005, 0, b), outside=geo_point_in_polygon(0.01, 0, b), bad=isnull(geo_point_buffer(0, 0, -1))`, `[[Polygon 1 0 1]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Errorf("%s\n got %s\nwant %s", tc.query, got, tc.want)
		}
	}
}
