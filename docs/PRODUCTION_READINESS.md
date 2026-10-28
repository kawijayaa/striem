# Production readiness

Status as verified on 2026-10-29. Each item names the check that verifies it.

## Build and deployment

| Item | Verified by |
|--|--|
| The Docker image builds from a clean context with KSQL as a build context, as CI does. | `docker build --build-context ksql=../ksql .` |
| The container runs as the unprivileged `striem` user, with data on a separate `/data` volume. | `docker exec … id` → `uid=100(striem)` |
| A mounted read-only challenge ingests, reports ready, and serves the UI and native console page. | Container smoke test: `/api/ready`, `/`, `/kusto.html` |
| Dataset and index paths in the config cannot escape the config directory or inject SQL. | `internal/deployment` tests, `ValidateIndexedPath` |

## Security

| Item | Verified by |
|--|--|
| State-changing requests need `X-Striem-Request`, plus same-origin `Sec-Fetch-Site` and `Origin`. | Container smoke test (403 without the header and for cross-site requests) |
| Every response sends a CSP, `X-Frame-Options: DENY`, `nosniff` and `no-referrer`. API responses are never cached. | `TestSecurityHeaders`; 50 browser tests pass under the CSP |
| User queries run on `query_only` connections and cannot write. | `TestQueryDatabaseIsReadOnly` |
| Queries see only catalog tables, not progress or SQLite internals. | Container smoke test: `table("investigation_progress")` is unknown |
| Answers are compared in constant time over SHA-256 hashes. The flag appears only when every task is solved. | `TestChallengeProgressAndCompletion` |
| Concurrent guesses cannot bypass the submission cooldown. | `TestSolvedQuestionsAndConcurrentGuesses` (30 parallel guesses record 1 attempt) |
| Request bodies, queries, answers and results are bounded: 64 KiB, 32 KiB, 512 characters, and 1,000 rows or 64 MB. Queries stop after 5 s. | API tests; `internal/api/server.go` limits |
| A query that outlives its 5-second deadline is interrupted repeatedly until it stops, so runaway queries cannot hold connections. The API stays responsive meanwhile, and limit errors explain themselves. | `TestRunawayQueriesStopAtTheirDeadline`, `TestQueryLimitErrorsAreExplained`, container test (6 runaway queries all stop at 5 s) |
| Hostile queries keep server memory bounded (318 MB peak across range, make_list, cross join and strrep attacks). | Local load test |
| The compiler does not panic or hang on hostile input. UDF panics are recovered. Regexes are RE2. | `FuzzCompileNeverPanics` (3.5M executions), `FuzzForensicUDFs`, `TestCompileTerminatesSafely` |
| `reduce`, `parse_user_agent`, `range`, `strrep` and sketches have input caps. | `TestReduceTermCap`, `TestParseUserAgent` |
| `has` with a per-row term escapes it with `regex_quote`, so data cannot inject regex syntax. | `TestStringOperatorSemantics` |
| The UI builds icons and results with DOM APIs, not HTML strings. | Code review: `web/src` has no `innerHTML` sinks |

## Correctness

| Item | Verified by |
|--|--|
| Microsoft's documented KQL examples, within the CTF scope (see `KQL_IMPLEMENTATION.md`). | Doc harness: 328 pass. Every failure and mismatch was reviewed and classified. |
| Integers beyond 2^53 reach the browser exactly, both top-level and inside dynamic values. | `TestUnsafeIntegersAreExact`, `web/tests/api.test.mjs`, browser walkthrough |
| Real division by zero gives ±∞, displayed as `Infinity`. | `TestArraySortColumnsAndDivision`, `TestScanRowsNormalizesNonFiniteNumbers` |
| A solved task reports later wrong answers as wrong. | `TestInvestigationQuestionWorkflow`, container smoke test |
| Screens render with no console errors: the workspace, results, JSON dialog, save-hunt dialog, hunts, all tasks, the completed challenge with its flag, mobile Query/Results/Investigate, and the native console. | Browser screenshot review |
| A completed challenge reports all tasks solved. The Kusto service link appears only when a service is configured. | `features.spec.ts`, `native-console.spec.ts` |
| UI flows work at desktop and mobile widths with no console errors or CSP violations. Covered flows: queries, results, sorting, pivots, the JSON viewer, the timeline, saved hunts, history, autocomplete, tasks and flag, error recovery, and the native console. | 50 Playwright tests and a browser walkthrough |

## Known limitations

- **NaN (accepted for production):** SQLite turns a NaN result into NULL, so `0.0/0.0` and `avg()` over an empty input are null rather than Kusto's NaN.
- **Old browsers:** browsers without `JSON.rawJSON` (before Chrome 114, Firefox 135 and Safari 18.4) show integers beyond 2^53 inside dynamic values as exact digit strings rather than numbers.
- **Publishing:** CI checks out KSQL at the commit pinned in the Docker workflows. Push KSQL before Striem whenever the pin moves.
- **Out of scope by decision:** KQL features outside the CTF scope report unsupported errors. These include geo, series, graph, external plugins, activity metrics, fork/facet and pattern mining. See `KQL_IMPLEMENTATION.md`.
