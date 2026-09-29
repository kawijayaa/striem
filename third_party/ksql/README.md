# ksql

`ksql` is a dependency-free Go library that parses Kusto Query Language (KQL)
and compiles relational KQL pipelines to SQL.

The language inventory is pinned to Microsoft's official
[Kusto-Query-Language](https://github.com/microsoft/Kusto-Query-Language)
source. KQL also contains control-plane, remote-execution, visualization,
graph, and plugin features that have no generic SQL equivalent. The compiler
returns stable diagnostics for those features rather than silently changing
their meaning.

## Install

```sh
go get github.com/kawijayaa/ksql
```

## Use

```go
compiler := ksql.New(dialect.PostgreSQL())
result := compiler.Compile(`
    Events
    | where severity >= 3
    | summarize errors=count() by service
    | top 10 by errors
`)
if !result.OK() {
    // Inspect result.Diagnostics. SQL is empty after any error.
}
fmt.Println(result.SQL)
```

Applications that need validation and ordered result metadata provide a
catalog. Secure placeholder rendering is opt-in:

```go
catalog := ksql.CatalogFunc(func(name string) (ksql.Table, bool) {
    if !strings.EqualFold(name, "Events") {
        return ksql.Table{}, false
    }
    return ksql.Table{Name: "events", Schema: ksql.Schema{Columns: []ksql.Column{
        {Name: "TimeGenerated", Type: ksql.TypeDateTime},
        {Name: "Host", Type: ksql.TypeString},
        {Name: "RawData", Type: ksql.TypeDynamic},
    }}}, true
})
compiler := ksql.New(
    dialect.SQLite(
        dialect.WithRegexFunction("kql_regex"),
        dialect.WithRegexCaseInsensitiveFlag("(?i)"),
    ),
    ksql.WithCatalog(catalog),
    ksql.WithParameters(),
)
result := compiler.Compile(`Events | where Host == "untrusted" | project Host`)
// result.SQL contains placeholders, result.Args contains values, and
// result.Columns is the ordered KQL result schema.
```

`WithRegexCaseInsensitiveFlag` is optional and should match the configured
regex engine. When omitted, SQLite case-insensitive regex rendering preserves
the existing behavior of lowercasing both operands.

Renderers are included for ANSI SQL, PostgreSQL, SQLite, MySQL 8, and SQL
Server. SQL is produced through a dialect-neutral AST; database-specific
identifier quoting, literals, regular expressions, JSON access, row limits,
safe casts, and range generation stay in the dialect package.

## Coverage

The built-in relational compiler currently lowers:

- schema-aware `where`/`filter`, `project`, `extend`, `summarize`, and
  `distinct`; `extend` replaces existing columns in place
- exact-name `project-away`, `project-keep`, `project-rename`, and
  `project-reorder`
- `count`, `sort`/`order`, `take`/`limit`, and `top`
- `sample`, `sample-distinct`, and reusable pipeline aliases with `as`
- `inner`, `leftouter`, `rightouter`, `fullouter`, `leftsemi`, and `leftanti`
  joins where supported by the selected database; bound joins emit explicit,
  deterministic collision aliases
- name/type-aligned outer and inner `union`, including standalone queries and ordered or limited operands
- single-array SQLite `mv-expand` and row-wise `mv-apply` subqueries using
  `where`, `extend`, and `serialize`; unsupported replacement, multi-array,
  and aggregating forms return diagnostics
- scalar and tabular `let` bindings
- table, `print`, `datatable`, and dialect-capable `range` sources
- arithmetic, comparisons, Boolean logic, membership, ranges, substring
  matching, regular expressions, conditionals, common aggregates, string
  functions, mathematics, typed literals, casts, and dynamic indexing
- schema-aware literal `search` through an explicit regex capability, plus
  SQLite/PostgreSQL `make_list` with dynamic result metadata

Every operator and built-in registry entry from the pinned Microsoft source is
recorded by the `features` package. Recognized features that cannot be
represented safely in the selected SQL dialect return a source-located stable
diagnostic. They never become an unchecked SQL function call.

See [`docs/operator-support.md`](docs/operator-support.md) for dialect-specific
operator behavior and current multi-value expansion limits.

Applications can explicitly supply database extensions or UDF mappings with
`WithFunction`, `WithOperator`, and `WithSource`. Source and operator rules
receive a `LoweringContext`, lower to `sqlast` nodes, and return a typed
`Relation`; source rules are invoked only after tabular `let` and `as`
resolution. See
[`docs/architecture.md`](docs/architecture.md) for the support model and
[`features/README.md`](features/README.md) for ledger semantics.

## Provenance

The language inventory is reconciled against Microsoft commit
`c94c9e761b3d095e2b9c21e3ec3e92afa0af4856` from July 19, 2026. The exact
grammar and catalog files are recorded in [`inventory/source.json`](inventory/source.json).

This project is an independent implementation. See [`NOTICE`](NOTICE).

## License

Apache-2.0. See [`NOTICE`](NOTICE) for upstream attribution.
