# Full KQL implementation

## Objective and completion standard

Implement all of KQL. This objective remains **incomplete**. No subset of operators, passing test suite, or syntax-only recognition establishes completion.

For each construct, evidence must cover syntax, binding/type inference, execution semantics (including nulls and invalid inputs), output schema/types, errors, and integration with Striem. Operator combinations, parameterized expressions, and boundary cases must be exercised. Documented approximation is not proof of exact Kusto compatibility.

## Authoritative inventory

The companion KSQL checkout has a feature ledger covering more than 650 entries. Its pinned Microsoft source is recorded in `third_party/ksql/inventory/source.json`. The pinned catalog includes 397 scalar functions, 24 conversion entries, 61 aggregates, 8 window entries, and 47 plugins, plus operators, sources, statements, and scalar operators. Registry entries can overlap.

`features.All()` and `features.JSON()` expose the baseline. The ledger distinguishes inventory, recognition, translation, and semantics. It is a starting inventory, **not evidence that everything marked equivalent has complete Kusto behavior**, and must be reconciled against the current Microsoft language/reference as implementation proceeds. Newer features remain in scope.

## Current integration

- The source of compiler development is the companion `ksql` repository.
- Striem builds against a portable snapshot in `third_party/ksql` using a module replacement. It does not depend on a developer's home-directory layout.
- `scripts/sync-ksql.py` refreshes source/tests/license/docs and records hashes and the upstream base commit. Changes in the companion checkout are included, even before release.
- SQLite runtime adapters remain in `internal/database`; application mappings and compatibility tests remain in `internal/kql`.
- The compiler's `not()` implementation now supplies Boolean metadata directly. The former application metadata-recovery workaround was removed.

## Verified implementation increments

- Default and explicit `innerunique`: one complete left row per equality-key tuple, preserved right multiplicity, internal rank-column collision avoidance, explicit/reversed side keys, composite keys, null/nonmatching keys, empty inputs, tabular bindings, and output schema.
- `rightsemi` / `rightanti`: right-only columns, retained right duplicates, null-key behavior, empty inputs, and correlated equality predicates.
- SQL window AST/renderer support with partition/order clauses and parameter traversal. This is infrastructure; it does not itself implement KQL window functions or serialization semantics.
- SQLite execution tests in the compiler and Striem; render checks for other supported SQL dialects. Render checks do not prove execution on those databases.
- Union aligns bound input schemas by name/type: outer null padding, distinct columns for conflicting types, suffix collision avoidance, inner intersection, duplicate retention, nested/standalone queries, and tabular bindings. Tests cover real event tables, Boolean expressions, empty legs, metadata, bound parameters, and projection limits.
- Union semantics reference: [Microsoft union operator](https://learn.microsoft.com/en-us/kusto/query/union-operator?view=microsoft-fabric). Unknown input schemas and unsupported options produce diagnostics. Wildcards, `withsource`, fuzzy resolution, zero-column outputs, case-distinct names on SQL backends, and exact int/long, decimal/real, guid/string distinctions remain open.

## Remaining workstreams (all stay in scope)

1. **Scalar semantics and types:** complete function catalog, Unicode/string behavior, all casts and null-on-failure behavior, dynamic values/literals, GUID/decimal/timespan types, scalar `let`/function definitions, datetime arithmetic, formatting/timezones, Boolean and null semantics.
2. **Aggregates:** full aggregate catalog; `arg_min`/`arg_max` row selection; conditional/dynamic collections; percentiles/statistics; genuine HLL-based approximate counts and mergeable states; ordering/default/empty-input behavior.
3. **Relational operators:** remaining union forms and complete scalar type distinctions, projection wildcards, parse variants, multi-array expansion, aggregating `mv-apply`, partition/scan, top-nested/top-hitters, series operators, find/search variants, schema operators, and all hints/parameters with their defined semantics.
4. **Ordering and windows:** serialized row identity/order through pipelines; row_number/rank/cumulative/neighbor/session functions and restart predicates.
5. **Sources, statements, and reusable queries:** all source forms; parameter declarations; functions/invoke; external data; database/cluster/table resolution; entity groups, macro expansion, materialization, session controls.
6. **Graphs and specialized scalar families:** graph construction/matching/path algorithms, geospatial/S2/H3 operations, series analysis, encodings/hashes, network functions, regex compatibility.
7. **Plugins and execution services:** evaluate plugins, their declared output schemas, and any required external runtimes/services. Unavailable services must not be silently replaced with invented results.
8. **Product integration:** completion/highlighting, API contracts, render directives/chart presentation, query diagnostics/cancellation/limits, browser execution, deployment and migration support.
9. **Conformance:** official documented examples, operator composition, differential fixtures against Kusto where available, adversarial/resource cases, platform coverage, current-catalog reconciliation, and a requirement-by-requirement completion audit.

Known existing semantic differences remain open: exact bounded `dcount` differs from Kusto approximation; `take_any` currently maps to a minimum; some SQLite conversions and string indexing differ from KQL; chronology/serialization and dynamic output metadata need broader review. Existing behavior must be corrected as part of full support, rather than declared sufficient.
