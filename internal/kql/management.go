package kql

import (
	"strings"

	"github.com/kawijayaa/ksql"
)

// Striem has one logical database namespace. Events is its aggregate view;
// deployment table names are dataset-filtered views of the same event store.
func managementCatalog(tables TableCatalog) ksql.ManagementCatalog {
	catalog := ksql.ManagementCatalog{DatabaseName: "striem"}
	names := []string{"Events"}
	for name := range tables {
		if !strings.EqualFold(name, "Events") {
			names = append(names, name)
		}
	}
	for _, name := range names {
		schema, _ := tables.schema(name)
		catalog.Tables = append(catalog.Tables, ksql.ManagementTable{Name: name, Schema: schema})
	}
	return catalog
}
