# Repository layout and development

The root `ksql` package owns the public compiler API. Keeping its implementation
in the same Go package preserves the public import path and types. Files are
grouped by compiler stage and feature rather than collecting unrelated stages
in one large file.

| Location | Responsibility |
| --- | --- |
| `compiler.go` | Public constructor, result, and compilation entry point |
| `compiler_state.go` | Per-compilation state, pipeline binding, feature recording |
| `binding_*.go` | Scalar expressions, names, literals, and scalar operators |
| `lowering_*.go` | SQL lowering for sources, operators, projections, joins, search, and expansion; extension context |
| `functions*.go`, `scalar_queries.go`, `materialized_tables.go`, `query_bindings.go` | Built-in/query-defined functions and materialized scalar/table queries |
| `schema.go`, `options.go`, `diagnostic.go`, `validation.go` | Public contracts, configuration, diagnostics, and validation |
| Other root feature files | Specialized union, join, schema, and column-pattern implementation |
| `kql/` | Lexer, parser, and KQL AST with package tests |
| `sqlast/` | Dialect-neutral SQL AST and traversal with package tests |
| `dialect/` | Database renderers and their tests |
| `features/`, `inventory/` | Support ledger and pinned Microsoft source metadata |
| `tests/compiler/` | Public API, schema, resource-limit, SQLite execution, and fuzz regression tests |
| `examples/` | Runnable library examples |
| `docs/` | Architecture, feature guides, support limits, and development |

## Tests

Run from the repository root:

```sh
go test ./...
go test -race ./...
go vet ./...
```

The SQLite execution tests use `go-sqlite3`, so they require CGO and a C compiler.
The production compiler itself does not import that test dependency.

For a focused compiler regression:

```sh
go test ./tests/compiler -run ToScalar -count=1
```

Public API tests are a separate package. To include the library implementation
in coverage, specify the packages to instrument explicitly:

```sh
go test -coverpkg=./... -coverprofile=coverage.out ./...
```

For a short compiler fuzzing run:

```sh
go test ./tests/compiler -run '^$' -fuzz '^FuzzQueryFunctions$' -fuzztime=10s
```

Go permits one fuzz target per invocation. Lexer/parser fuzz targets remain in
`kql/`.

## Adding code

Keep compiler entry-point wiring in `compiler.go`. Add scalar binding logic to
an appropriate `binding_` file and tabular SQL lowering to a `lowering_` or
feature-specific file. Keep related helpers together; add a focused file when
a feature would otherwise span unrelated responsibilities.

Place public API and execution regressions in `tests/compiler/`. The catalog
fixtures currently live in `binding_test.go`, and shared SQLite driver/query
helpers in `sqlite_execution_test.go`, in that directory. Internal package tests
stay beside their implementations. Use `go test ./...` to include both groups;
`go test .` only builds the root package.

Update the applicable guide in `docs/` when support changes. Keep the README
focused on installation, basic usage, and links to detailed guidance. Run
`gofmt` on changed Go files and keep semantic changes separate from layout-only
refactors.
