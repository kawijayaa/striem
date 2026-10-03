# Materialized tables

Microsoft's [`materialize()`](https://learn.microsoft.com/en-us/kusto/query/materialize-function?view=microsoft-fabric)
captures a tabular expression once during a query execution. Its rows and
columns can then be reused across branches without recalculation:

```kql
let cached = materialize(Events | where severity >= 3);
union (cached | summarize errors=count() by service),
      (cached | summarize errors=count() by host)
```

The [`as` operator](https://learn.microsoft.com/en-us/kusto/query/as-operator?view=microsoft-fabric)
provides the same behavior with `hint.materialized=true`:

```kql
Events
| as hint.materialized=true cached
| join kind=inner (cached) on id
```

Without materialization, ordinary table `let` bindings and `as` aliases
recalculate their expressions on each reference. Explicitly setting
`hint.materialized=false` preserves that behavior.

## Implementation and verification

SQLite 3.35+ and PostgreSQL 12+ render reachable caches as `AS MATERIALIZED`
CTEs, shared with `toscalar()` bindings. Dependencies precede their consumers.
Cached tables retain all rows, ordered column metadata, and scalar types.
Source extensions and catalogs apply normally within the cached expression.
Unused bindings and function declaration-validation placeholders are omitted;
queries do not evaluate unused caches. Parameters follow SQL rendering order,
and generated binding names avoid collisions with physical tables.

SQLite execution tests verify union/join reuse using a nondeterministic
counting function, repeated executions, nested caches, table functions,
scalar-cache dependencies, empty tables, typed/quoted columns and ordinary
binding recalculation. PostgreSQL has rendering and parameter checks only.
Compiler policies bound the number of materialized bindings and cumulative
expanded SQL work; these are not Kusto runtime cache limits.

## Remaining conformance work

- Query-wide SQL CTEs cannot represent caches dependent on caller rows.
  Such inputs produce an explicit unsupported lowering diagnostic; this is
  a backend limit, not a claim that the expression is invalid KQL.
- Kusto's 5 GB per-node cache shared across concurrent queries requires
  an external execution runtime; this library does not reproduce it.
- Graph expressions, partition materialization, multiple-result batches,
  unsupported SQL dialects and Kusto engine differential verification remain
  open. Row order still requires an explicit final ordering.
