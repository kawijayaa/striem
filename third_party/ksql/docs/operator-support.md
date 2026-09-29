# Operator support

Support is recorded in separate dimensions. Syntax recognition does not imply
binding, SQL lowering, or executable semantics. Unsupported forms return a
positioned diagnostic and no partial SQL.

| Construct | Syntax | Typed AST | Bound | SQL lowered | Executed dialects | Semantics |
| --- | --- | --- | --- | --- | --- | --- |
| `where`, `project`, `extend`, `distinct` | Yes | Yes | Yes with catalog | Yes | SQLite; renderer tests for PostgreSQL/MySQL/SQL Server | Equivalent for covered scalar expressions |
| `summarize` core aggregates | Yes | Yes | Yes with catalog | Yes | SQLite core; renderer tests elsewhere | Equivalent subject to database null/numeric behavior |
| `make_list` | Yes | Yes | Yes | Yes | SQLite; PostgreSQL rendering | Lossy: result order is arbitrary without an explicit database order |
| `sort`, `top`, `take`, `count` | Yes | Yes | Yes | Yes | SQLite and renderer tests | Equivalent; configured constant row bounds are enforced |
| `project-away`, `project-keep` | Yes | Expression list | Yes | Yes | SQLite | Exact names and case-sensitive `*` patterns; input column order retained |
| `project-rename` | Yes | Named expressions | Yes | Yes | SQLite | Simultaneous, case-insensitive rename with collision checks |
| `project-reorder` | Yes | Expression list | Yes | Yes | SQLite | Exact names and `*` patterns; ordinal and natural numeric sorting; first match wins; unspecified columns retain input order |
| `search` | Yes | `SearchSpec` | Yes | Capability-dependent | Yes | SQLite with UDF | Equivalent for one non-empty literal string; regex metacharacters are escaped and wide inputs can be bounded |
| `join kind=inner` | Yes | `JoinSpec` | Yes | Yes | SQLite | Explicit KQL output aliases; right collisions use deterministic numeric suffixes |
| `leftouter`, `rightouter`, `fullouter` | Yes | `JoinSpec` | Yes | Yes | Renderer-dependent | Database join behavior; SQLite version must support requested join |
| `leftsemi`, `leftanti` | Yes | `JoinSpec` | Yes | `EXISTS`/`NOT EXISTS` | SQLite (`leftanti`) | Equivalent for supported key predicates |
| `rightsemi`, `rightanti` | Yes | `JoinSpec` | Yes | `EXISTS`/`NOT EXISTS` | SQLite | Right-only schema; right duplicates preserved; null keys do not match |
| `innerunique` (default join) | Yes | `JoinSpec` | Bound left schema | Partitioned `ROW_NUMBER` then inner join | SQLite; other dialect renderer checks | One complete left representative per equality-key tuple; representative is unspecified |
| `lookup` | Yes | `JoinSpec` | Yes | Yes | Renderer tests | Right same-name columns are omitted; size hints are not enforced |
| `union` (pipeline or standalone) | Yes | `UnionSpec` | Bound schemas | Name/type-aligned `UNION ALL` | SQLite | Outer null padding and type suffixes with collision avoidance; inner intersects name/type pairs; preserves duplicates |
| `mv-expand` | Yes | `MvExpandSpec` | Yes | One JSON value | SQLite JSON1 | Lossy SQLite casts; arrays/objects tested, multiple expansions unsupported |
| `mv-apply` row-wise | Yes | `MvApplySpec` | Yes | `where`, `extend`, `serialize` | SQLite JSON1 | Equivalent for documented row-wise subset |
| `mv-apply` aggregating | Yes | `MvApplySpec` | No | No | None | Unsupported with `KQLL0334` |
| `parse`, `parse-where`, `parse-kv` | Yes | Opaque | No | No | None | Unsupported; no unchecked UDF lowering |
| `evaluate bag_unpack` | Yes | Opaque | No | No | None | Unsupported; schema declaration is not yet typed |
| `dcount` | Yes | Call expression | Diagnostic | No default | None | Approximate KQL semantics require an explicit compatible mapping |
| `arg_min`, `arg_max` | Yes | Call expression | Diagnostic | No | None | Schema-aware window lowering remains unsupported |

## SQLite details

SQLite dynamic string literals without double quotes or backslashes lower
through `json_extract` with quoted labels such as `$."a.b"` and
`$."space key"`. Consecutive safe properties collapse into one path. Keys with
double quotes or backslashes, and indexes rendered as parameters, retain the
`json_each(... WHERE key = ...)` fallback. Numeric indexes lower through JSON
paths of the form `$[0]`. Nested accesses retain the input alias inside joins
and JSON expansion.

SQLite union operands are rendered without invalid top-level parentheses.
Operands containing `ORDER BY`, `LIMIT`, or nested unions are wrapped as derived
queries and execute inside joins and tabular bindings.

`== null` lowers to `IS NULL`; `!= null` and `<> null` lower to `IS NOT NULL`.
JSON null expands to one null row, while an empty array expands to no rows.

Stock SQLite has no KQL-compatible regular expression implementation. Configure
an application UDF explicitly:

```go
dialect.SQLite(
    dialect.WithRegexFunction("kql_regex"),
    dialect.WithRegexCaseInsensitiveFlag("(?i)"),
)
```

The UDF receives `(pattern, value)`. Function names are validated as SQL
identifiers. The case-insensitive flag is optional and must be supported by the
configured regex engine. When set, it prefixes case-insensitive patterns rather
than lowercasing either operand; callers that omit it retain operand
lowercasing. Without `WithRegexFunction`, `search`, `has`, and regex operators
return a diagnostic; they are never lowered to `LIKE`.

## Known gaps

- Column-selection patterns are supported by `project-away`, `project-keep`, and `project-reorder`. Zero-column outputs remain unsupported; exact-name binding remains case-insensitive. Numeric sorting is verified for ASCII digit runs; Unicode digit collation still needs conformance coverage.
- Parse operators, schema-declared
  `bag_unpack`, `arg_min`, `arg_max`, and aggregating `mv-apply` are unsupported.
- Multiple-array expansion is rejected.
- Union wildcard sources, `withsource`, fuzzy resolution, and zero-column results remain unsupported. SQL backends cannot represent columns differing only in case. The existing type model still conflates decimal/real and guid/string; those distinctions remain pending.
- SQLite `to typeof(...)` casts do not provide KQL null-on-failure semantics.
- `MaxInputRowsPerStage` and `MaxParseCaptures` are reserved until supported
  operators can enforce them without claiming execution guarantees.
