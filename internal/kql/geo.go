package kql

import (
	"fmt"

	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/sqlast"
)

// geoFunction describes a geo_* call: its runtime function, the kind of each
// parameter (n numeric, d dynamic, s string, b bool), how many are required
// and the result type. Omitted optional parameters take Kusto's defaults
// (an explicit null stays null); those without a default are passed as NULL.
type geoFunction struct {
	sql      string
	params   string
	required int
	result   ksql.ScalarType
	defaults map[int]sqlast.Expr
}

func geoInt(value string) sqlast.Expr {
	return &sqlast.Literal{Kind: sqlast.NumberLiteral, Value: value}
}
func geoReal(value string) sqlast.Expr {
	return &sqlast.Literal{Kind: sqlast.NumberLiteral, Value: value}
}

var geoFunctions = map[string]geoFunction{
	"geo_distance_2points":               {"kql_geo_distance_2points", "nnnnb", 4, ksql.TypeReal, map[int]sqlast.Expr{4: geoInt("0")}},
	"geo_azimuth":                        {"kql_geo_azimuth", "nnnn", 4, ksql.TypeReal, nil},
	"geo_angle":                          {"kql_geo_angle", "nnnnnn", 6, ksql.TypeReal, nil},
	"geo_point_in_circle":                {"kql_geo_point_in_circle", "nnnnn", 5, ksql.TypeBool, nil},
	"geo_point_in_polygon":               {"kql_geo_point_in_polygon", "nnd", 3, ksql.TypeBool, nil},
	"geo_line_length":                    {"kql_geo_line_length", "d", 1, ksql.TypeReal, nil},
	"geo_line_centroid":                  {"kql_geo_line_centroid", "d", 1, ksql.TypeDynamic, nil},
	"geo_polygon_area":                   {"kql_geo_polygon_area", "d", 1, ksql.TypeReal, nil},
	"geo_polygon_perimeter":              {"kql_geo_polygon_perimeter", "d", 1, ksql.TypeReal, nil},
	"geo_polygon_centroid":               {"kql_geo_polygon_centroid", "d", 1, ksql.TypeDynamic, nil},
	"geo_distance_point_to_line":         {"kql_geo_distance_point_to_line", "nnd", 3, ksql.TypeReal, nil},
	"geo_closest_point_on_line":          {"kql_geo_closest_point_on_line", "nnd", 3, ksql.TypeDynamic, nil},
	"geo_distance_point_to_polygon":      {"kql_geo_distance_point_to_polygon", "nnd", 3, ksql.TypeReal, nil},
	"geo_closest_point_on_polygon":       {"kql_geo_closest_point_on_polygon", "nnd", 3, ksql.TypeDynamic, nil},
	"geo_line_interpolate_point":         {"kql_geo_line_interpolate_point", "dn", 2, ksql.TypeDynamic, nil},
	"geo_line_locate_point":              {"kql_geo_line_locate_point", "dnn", 3, ksql.TypeReal, nil},
	"geo_line_densify":                   {"kql_geo_line_densify", "dnb", 1, ksql.TypeDynamic, map[int]sqlast.Expr{1: geoReal("10"), 2: geoInt("0")}},
	"geo_polygon_densify":                {"kql_geo_polygon_densify", "dnb", 1, ksql.TypeDynamic, map[int]sqlast.Expr{1: geoReal("10"), 2: geoInt("0")}},
	"geo_line_simplify":                  {"kql_geo_line_simplify", "dn", 1, ksql.TypeDynamic, map[int]sqlast.Expr{1: geoReal("10")}},
	"geo_polygon_simplify":               {"kql_geo_polygon_simplify", "dn", 1, ksql.TypeDynamic, map[int]sqlast.Expr{1: geoReal("10")}},
	"geo_intersects_2lines":              {"kql_geo_intersects_2lines", "dd", 2, ksql.TypeBool, nil},
	"geo_intersects_line_with_polygon":   {"kql_geo_intersects_line_with_polygon", "dd", 2, ksql.TypeBool, nil},
	"geo_intersects_2polygons":           {"kql_geo_intersects_2polygons", "dd", 2, ksql.TypeBool, nil},
	"geo_point_to_geohash":               {"kql_geo_point_to_geohash", "nnn", 2, ksql.TypeString, map[int]sqlast.Expr{2: geoInt("5")}},
	"geo_geohash_to_central_point":       {"kql_geo_geohash_to_central_point", "s", 1, ksql.TypeDynamic, nil},
	"geo_geohash_to_polygon":             {"kql_geo_geohash_to_polygon", "s", 1, ksql.TypeDynamic, nil},
	"geo_geohash_neighbors":              {"kql_geo_geohash_neighbors", "s", 1, ksql.TypeDynamic, nil},
	"geo_point_to_s2cell":                {"kql_geo_point_to_s2cell", "nnn", 2, ksql.TypeString, map[int]sqlast.Expr{2: geoInt("11")}},
	"geo_s2cell_to_central_point":        {"kql_geo_s2cell_to_central_point", "s", 1, ksql.TypeDynamic, nil},
	"geo_s2cell_to_polygon":              {"kql_geo_s2cell_to_polygon", "s", 1, ksql.TypeDynamic, nil},
	"geo_polygon_to_s2cells":             {"kql_geo_polygon_to_s2cells", "dnn", 1, ksql.TypeDynamic, map[int]sqlast.Expr{1: geoInt("11"), 2: geoInt("0")}},
	"geo_from_wkt":                       {"kql_geo_from_wkt", "s", 1, ksql.TypeDynamic, nil},
	"geo_point_to_h3cell":                {"kql_geo_point_to_h3cell", "nnn", 2, ksql.TypeString, map[int]sqlast.Expr{2: geoInt("6")}},
	"geo_h3cell_level":                   {"kql_geo_h3cell_level", "s", 1, ksql.TypeInt, nil},
	"geo_h3cell_parent":                  {"kql_geo_h3cell_parent", "sn", 1, ksql.TypeString, nil},
	"geo_h3cell_children":                {"kql_geo_h3cell_children", "sn", 1, ksql.TypeDynamic, nil},
	"geo_h3cell_neighbors":               {"kql_geo_h3cell_neighbors", "s", 1, ksql.TypeDynamic, nil},
	"geo_h3cell_rings":                   {"kql_geo_h3cell_rings", "sn", 2, ksql.TypeDynamic, nil},
	"geo_h3cell_to_central_point":        {"kql_geo_h3cell_to_central_point", "s", 1, ksql.TypeDynamic, nil},
	"geo_h3cell_to_polygon":              {"kql_geo_h3cell_to_polygon", "s", 1, ksql.TypeDynamic, nil},
	"geo_polygon_to_h3cells":             {"kql_geo_polygon_to_h3cells", "dnn", 1, ksql.TypeDynamic, map[int]sqlast.Expr{1: geoInt("6"), 2: geoInt("0")}},
	"geo_intersection_2polygons":         {"kql_geo_intersection_2polygons", "dd", 2, ksql.TypeDynamic, nil},
	"geo_intersection_2lines":            {"kql_geo_intersection_2lines", "dd", 2, ksql.TypeDynamic, nil},
	"geo_intersection_line_with_polygon": {"kql_geo_intersection_line_with_polygon", "dd", 2, ksql.TypeDynamic, nil},
	"geo_union_polygons_array":           {"kql_geo_union_polygons_array", "d", 1, ksql.TypeDynamic, nil},
	"geo_union_lines_array":              {"kql_geo_union_lines_array", "d", 1, ksql.TypeDynamic, nil},
	"geo_simplify_polygons_array":        {"kql_geo_simplify_polygons_array", "dn", 1, ksql.TypeDynamic, map[int]sqlast.Expr{1: geoReal("10")}},
	"geo_point_buffer":                   {"kql_geo_point_buffer", "nnnn", 3, ksql.TypeDynamic, map[int]sqlast.Expr{3: geoReal("0.1")}},
	"geo_s2cell_neighbors":               {"kql_geo_s2cell_neighbors", "s", 1, ksql.TypeDynamic, nil},
}

func geoParameterAccepts(kind byte, t ksql.ScalarType) bool {
	if t == ksql.TypeUnknown {
		return true
	}
	switch kind {
	case 'n':
		return numericType(t)
	case 'd':
		return t == ksql.TypeDynamic
	case 's':
		return t == ksql.TypeString
	case 'b':
		return t == ksql.TypeBool
	}
	return false
}

func geoOptions() []ksql.Option {
	var options []ksql.Option
	for name, function := range geoFunctions {
		name, function := name, function
		options = append(options, ksql.WithTypedFunction(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) < function.required || len(args) > len(function.params) {
				return ksql.TypedExpression{}, fmt.Errorf("%s takes %d to %d arguments", name, function.required, len(function.params))
			}
			values := make([]sqlast.Expr, len(function.params))
			for i := range values {
				values[i] = &sqlast.Literal{Kind: sqlast.NullLiteral}
				if fallback, ok := function.defaults[i]; ok {
					values[i] = fallback
				}
				if i < len(args) {
					if !geoParameterAccepts(function.params[i], args[i].Type) {
						return ksql.TypedExpression{}, fmt.Errorf("%s argument %d has type %s", name, i+1, args[i].Type)
					}
					values[i] = args[i].SQL
				}
			}
			return ksql.TypedExpression{Type: function.result, SQL: &sqlast.Call{Name: function.sql, Args: values}}, nil
		}))
	}
	return options
}
