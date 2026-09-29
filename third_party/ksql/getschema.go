package ksql

import (
	"fmt"
	"github.com/kawijayaa/ksql/kql"
	"github.com/kawijayaa/ksql/sqlast"
)

func (s *compileState) getSchema(input Schema, operator kql.Operator) Relation {
	if raw, ok := operator.Body.(kql.RawSpec); !ok || len(raw.Tokens) != 0 {
		s.error("KQLL0360", operator.Span, "operator.getschema", "getschema parameters are not yet supported")
		return Relation{}
	}
	if input.Unknown {
		s.bindError("KQLB0360", operator.Span, "operator.getschema", "getschema requires a bound input schema")
		return Relation{}
	}
	schema := Schema{Columns: []Column{{Name: "ColumnName", Type: TypeString}, {Name: "ColumnOrdinal", Type: TypeLong}, {Name: "DataType", Type: TypeString}, {Name: "ColumnType", Type: TypeString}}}
	values := &sqlast.Values{Columns: []string{"ColumnName", "ColumnOrdinal", "DataType", "ColumnType"}, Alias: s.nextAlias()}
	types := map[ScalarType]string{TypeBool: "System.Boolean", TypeInt: "System.Int32", TypeLong: "System.Int64", TypeReal: "System.Double", TypeString: "System.String", TypeDateTime: "System.DateTime", TypeDynamic: "System.Object"}
	for i, column := range input.Columns {
		dataType, ok := types[column.Type]
		if !ok {
			s.bindError("KQLB0361", operator.Span, "operator.getschema", fmt.Sprintf("getschema cannot describe unknown type for column %s", column.Name))
			return Relation{}
		}
		values.Rows = append(values.Rows, []sqlast.Expr{s.bind(column.Name), s.bind(int64(i)), s.bind(dataType), s.bind(string(column.Type))})
	}
	return Relation{Query: &sqlast.Select{From: values}, Schema: schema}
}
