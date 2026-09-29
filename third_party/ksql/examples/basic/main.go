package main

import (
	"fmt"
	"log"

	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/dialect"
)

func main() {
	compiler := ksql.New(dialect.PostgreSQL())
	result := compiler.Compile(`
Events
| where severity >= 3
| summarize errors=count() by service
| top 10 by errors
`)
	if !result.OK() {
		log.Fatal(result.Diagnostics)
	}
	fmt.Println(result.SQL)
}
