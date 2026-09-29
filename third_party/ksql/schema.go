package ksql

import (
	"strings"

	"github.com/kawijayaa/ksql/sqlast"
)

// ScalarType is the KQL type of a scalar expression or result column.
type ScalarType string

const (
	TypeUnknown  ScalarType = "unknown"
	TypeBool     ScalarType = "bool"
	TypeInt      ScalarType = "int"
	TypeLong     ScalarType = "long"
	TypeReal     ScalarType = "real"
	TypeString   ScalarType = "string"
	TypeDateTime ScalarType = "datetime"
	TypeDynamic  ScalarType = "dynamic"
)

// Column describes one ordered relation column.
type Column struct {
	Name string
	Type ScalarType
}

// Schema is an ordered set of relation columns. Unknown is used by legacy,
// schema-less sources; validation requiring a complete column set is disabled
// for such a schema.
type Schema struct {
	Columns []Column
	Unknown bool
}

// Clone returns an independent copy of the schema.
func (s Schema) Clone() Schema {
	return Schema{Columns: append([]Column(nil), s.Columns...), Unknown: s.Unknown}
}

// Lookup resolves a KQL column name case-insensitively. The final return value
// reports an ambiguous schema (normally caused by an invalid catalog entry).
func (s Schema) Lookup(name string) (Column, int, bool, bool) {
	index := -1
	var result Column
	for i, column := range s.Columns {
		if strings.EqualFold(column.Name, name) {
			if index >= 0 {
				return Column{}, -1, false, true
			}
			index, result = i, column
		}
	}
	return result, index, index >= 0, false
}

// Table is a catalog relation. Name is the physical SQL table name used by the
// generic source rule; applications may override source lowering.
type Table struct {
	Name   string
	Schema Schema
}

// Catalog resolves logical KQL table names. Implementations must be safe for
// concurrent reads when shared by a Compiler.
type Catalog interface {
	ResolveTable(name string) (Table, bool)
}

// CatalogFunc adapts a function to Catalog.
type CatalogFunc func(name string) (Table, bool)

func (f CatalogFunc) ResolveTable(name string) (Table, bool) { return f(name) }

// Relation is a lowered SQL query paired with its ordered KQL output schema.
type Relation struct {
	Query  sqlast.Query
	Schema Schema
}

// Limits bounds syntax-driven compiler expansion. Zero means unlimited. These
// limits do not replace database execution limits or cancellation.
type Limits struct {
	MaxSourceBytes       int
	MaxOutputRows        int
	MaxProjectionItems   int
	MaxExpansionItems    int
	MaxInputRowsPerStage int
	MaxParseCaptures     int
	MaxRegexBytes        int
	MaxJoinKeys          int
}
