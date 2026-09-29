package ksql_test

import (
	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/dialect"
	"testing"
)

func TestMappedBagAggregatesHaveDynamicMetadata(t *testing.T) {
	for _, name := range []string{"make_bag", "make_bag_if", "make_dictionary"} {
		compiler := ksql.New(dialect.SQLite(), ksql.WithFunction(name, ksql.SQLFunction("custom_bag")))
		args := "x"
		if name == "make_bag_if" {
			args += ",true"
		}
		result := compiler.Compile(`print x=1 | summarize Bag=` + name + `(` + args + `) | project Renamed=Bag`)
		if !result.OK() || len(result.Columns) != 1 || result.Columns[0].Type != ksql.TypeDynamic {
			t.Fatalf("%s: %#v", name, result)
		}
	}
}
