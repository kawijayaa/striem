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
- scalar and tabular `let` bindings, plus scalar/table query-defined functions with
  typed parameters, literal defaults, named arguments, lexical captures,
  local declarations, nested functions, table parameters, and named `invoke`
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

## Scalar query-defined functions

```kusto
let adjust = (value:long, offset:long = 2) {
    let increment = () { offset };
    value + increment()
};
print result = adjust(offset=3, value=10)
```

Function calls inline a bound scalar expression and retain its output type.
Arguments resolve in the caller; bodies resolve in their declaration scope.
Parameters and local declarations stay local. Validation also checks unused
function declarations. Scalar parameter types currently supported are bool,
int, long, real/double, string, datetime, and dynamic; numeric widening supports
int to long and int/long to real. Unsupported types,
recursive or unresolved functions, invalid defaults, and invalid argument
contracts produce diagnostics with no SQL.

Declaration/call depth, parameter count, binding work, and expanded SQL work
are bounded. Shared argument trees are counted at their expanded size before
parameter traversal/rendering, preventing exponentially large SQL from small
function definitions. These are compiler resource policies. Full Microsoft
function type coercion, views/stored functions, inline invocation, expression result
naming, and engine conformance remain unfinished.

## Tabular query-defined functions

```kusto
let selectRecent = (T:(x:long), minimum:long = 2) {
    T | where x >= minimum
};
range x from 1 to 4 step 1
| invoke selectRecent()
```

Table-valued results can be used as sources, local bindings, and table
arguments. Zero-parameter functions support both `f` and `f()`. Table
parameters precede scalar parameters and use a declared column schema or
`(*)`. Fixed schemas validate required columns and types while retaining extra
argument columns; wildcard parameters permit star operations but cannot name
input columns during declaration validation. Table expressions such as
`selectRecent((range x from 1 to 4 step 1), 3)` are supported, along with direct
datatable arguments, lexical table capture, and nested function declarations.
`invoke` supplies its input as the first table argument. Both scalar and table
expansion share the resource limits described above. Exact coercion, case
resolution, schema-dependent function analysis and engine differential testing
are still incomplete.

Quoted column declarations use bracketed string names, including compound
strings: `datatable(['some' ' column']:long)[1]`. Output aliases in print,
project, extend, summarize, and project-rename decode the same form and retain
their names in result metadata. Dots inside quoted column names stay part of
the name; `['a.b']` is distinct from the property access `a.b`. SQL identifiers
are quoted by the target dialect. Complete entity-name validation, exact case
resolution, and all projection naming rules remain unfinished.

Join `on` conditions bind common column names or `$left.column ==
$right.column` pairs, combined with commas or `and`. Bracketed quoted columns
work in both forms. Explicit operands must reference actual columns on the
specified side; computed expressions, reversed sides, and dynamic explicit
keys return diagnostics. Project a scalar expression before using it as a key.
Legacy Boolean `join where` conditions remain supported for the join flavors
that can lower them. Lookup supports inner/leftouter and omits only its
right-side matching key columns, preserving other same-name columns with
generated names. Lookup currently requires known schemas for correct column
omission. Key-count limits include conjunctions, and join output width is
checked against the configured projection limit. Complete hints/parameters,
case resolution, unknown schemas, dynamic shorthand keys, and engine
differential conformance remain open.

## Constant scalar queries

```kusto
let total = toscalar(Events | count);
Events | summarize groupCount=count() by Host
       | extend fraction=1.0*groupCount/total
```

`toscalar()` accepts a scalar expression or a table expression; a table supplies
its first column and first row. Materialized CTEs make reused scalar bindings
constant across rows. SQLite 3.35+ and PostgreSQL 12+ support this lowering;
PostgreSQL is covered by rendering tests, SQLite by execution tests. Other
SQL dialects return a positioned capability diagnostic. Referencing a caller's
row is rejected, including through functions or previously bound tables.
Independent rows within the argument's own pipeline remain available.

The first table column must have a known schema. Empty tables currently return
SQL null; engine differential verification remains open. Optional `kind`
syntax and full Kusto scalar typing remain unfinished. At most 1024 bindings
and 4 MiB of estimated binding SQL work are accepted per compilation; these
are compiler policies, not Kusto limits. Unused lets/function-validation
bindings are removed before rendering and parameter normalization.
