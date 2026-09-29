// Package ksql converts Kusto Query Language queries to SQL.
//
// Compilation is translation-total: valid supported input produces SQL, and
// unsupported KQL functionality produces structured diagnostics. The package
// never silently passes unknown KQL functions or operators through to SQL.
package ksql
