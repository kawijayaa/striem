# Architecture

## Compilation stages

1. `kql.Lex` creates source-located tokens. KQL keywords remain contextual.
2. `kql.Parse` creates typed scripts, pipelines, core operator bodies, and
   recognized raw bodies for advanced operators.
3. Catalog binding resolves physical tables, scalar/tabular `let` bindings,
   `as` aliases, columns, dynamic property access, and join-side scopes.
4. Each bound pipeline stage produces a `Relation` containing a `sqlast.Query`
   and an ordered `Schema`. Expressions are type-checked before their SQL AST
   nodes are attached to that stage.
5. The `dialect` package renders the SQL AST for a concrete database.
6. The compiler returns SQL only when no error diagnostic exists.

Pipeline stages lower to nested derived tables. This is intentionally verbose:
it preserves KQL evaluation order and projection alias visibility. A future
optimizer may flatten only transformations proven to preserve those semantics.

## Support contract

KQL is larger than relational SQL. It includes remote HTTP/database access,
Python/R/C# execution, AI calls, visualizations, graph traversal, stateful scan
steps, control-plane commands, and Kusto-specific distributed execution hints.
No generic SQL text can reproduce those capabilities.

The library therefore distinguishes:

- **Inventory coverage:** the feature has a stable ledger entry.
- **Parse coverage:** syntax is validated, recognized, or retained as opaque.
- **Translation coverage:** compilation returns SQL or a stable diagnostic.
- **Semantic support:** translation is exact, equivalent, lossy, or unsupported.

Lossy behavior is documented rather than presented as equivalent. For example,
KQL `has` is lowered only for literal terms on regex-capable dialects and is
never silently rewritten to SQL `LIKE`; approximate `dcount` is not silently
changed to exact `COUNT(DISTINCT ...)`.

## Binding contract

Catalog lookup and KQL column lookup are case-insensitive. Catalog column names
remain case-preserving in result metadata. A dotted expression is resolved by
scope, not capitalization: if its first component is a dynamic column, the
remaining components become JSON property access; `$left` and `$right` resolve
only inside joins. Unknown, ambiguous, and duplicate names are bind errors with
source spans.

Catalogs are optional for compatibility. A source without a catalog has an
unknown/open schema: SQL lowering remains available, but validation or
operators requiring enumeration (`search` and schema projections) reject that
input. `Result.Columns` is complete only when the final schema is known.

## Extension rules

Extensions are explicit compiler options and receive SQL AST nodes, not SQL
fragments:

```go
compiler := ksql.New(
    dialect.PostgreSQL(),
    ksql.WithFunction("hash_sha256", ksql.SQLFunction("digest_sha256")),
)
```

`WithOperator` receives a `LoweringContext`, the preceding typed `Relation`, and
the parsed operator. `WithSource` receives a context and parsed source. Rules
can lower expressions or nested pipelines, bind parameters, allocate aliases,
read schemas and the catalog, add positioned diagnostics, and inspect the
selected dialect. They return SQL AST values, never SQL fragments. Tabular
bindings are resolved before a source rule, so a physical source adapter cannot
accidentally intercept `let` or `as` names.

Compiler options and rule maps are fixed by `New`; per-compilation aliases,
arguments, diagnostics, and bindings live in isolated state. A configured
compiler is safe for concurrent use if its catalog is safe for concurrent
reads.

## Parameters and limits

Literal rendering remains the default for compatibility. `WithParameters()`
turns KQL scalar literals and extension `Bind` values into dialect placeholders
and returns values in `Result.Args`. Arguments are allocated in SQL traversal
order across nested relations. Identifiers, SQL types, function names, and sort
directions are structural AST values and are never parameters.

`WithLimits` configures syntax-driven bounds. Zero and negative values mean
unlimited. `MaxSourceBytes`, `MaxOutputRows`, `MaxProjectionItems`,
`MaxExpansionItems`, `MaxRegexBytes`, and `MaxJoinKeys` are enforced by current
lowerings. `MaxInputRowsPerStage`, `MaxParseCaptures`, and execution-time row or
memory limits are reserved for operators that can enforce them accurately;
applications must still configure database cancellation and resource limits.

## Updating KQL

1. Update `inventory/source.json` to the audited Microsoft commit.
2. Reconcile query syntax against `QueryParser.cs`, `QueryGrammar.cs`, and
   `SyntaxNodeInfos.cs`.
3. Reconcile functions, aggregates, plugins, and parameters against the catalog
   files listed in `inventory/source.json`.
4. Add every new construct to `features` before adding lowering.
5. Add parser, lowering, dialect, and negative tests as appropriate.
