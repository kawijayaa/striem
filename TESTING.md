# Feature verification

The feature audit exercises documented capabilities using deterministic fixtures, actual SQLite execution, HTTP integration tests, and Chromium browser workflows. Passing these tests does not establish that every possible query, input file, browser, or deployment is bug-free.

## Run the checks

```sh
npm ci
npm test
npm run build
go test ./...
go test -race -tags sqlite_fts5 ./...
npx playwright install chromium
npm run test:e2e
```

The browser suite starts two isolated servers on ports 18081 and 18082. Each server uses a temporary database and generated NDJSON, gzip JSON-array, CSV, and EVTX fixtures. Normal and challenge workspaces are tested independently. The servers and their temporary directories are cleaned up after the run. Chromium can optionally be selected with `STRIEM_TEST_CHROMIUM=/absolute/path/to/chrome`.

For the Docker checks:

```sh
docker build -t striem-feature-test .
python3 tests/e2e/docker-smoke.py
```

The smoke test creates a uniquely named, non-root container with a read-only root filesystem, no external network, a temporary configuration, and an anonymous data volume. It checks readiness, HTML/fonts, FTS queries, challenge completion, and persistence across restart. It removes the container and its volume afterwards; the local test image remains available.

## Coverage

| Area | Verification |
| --- | --- |
| Ingestion | Existing Go tests cover JSON arrays, NDJSON, CSV, EVTX, gzip variants, source/time mappings, embedded JSON, field discovery, size limits, rollback, and record order. Browser fixtures additionally exercise four formats through process startup and HTTP queries. |
| Deployment | Relative paths, file replacement/reuse, removed datasets, shared tables, field indexes, unsafe paths, invalid manifests/questions, optional challenges, and FTS recovery. |
| KQL | Execution matrix covers every documented tabular operator, all documented join/lookup kinds, variables, aggregates, casts, dynamic fields, string/IP/decoding helpers, membership, ranges, and clocks. Existing tests cover diagnostics, limits, unsupported lowering, and FTS. |
| APIs | All data routes, readiness/loading/error transitions, health, schema/fields, validation/execution, cancellation, malformed requests, CSRF checks, methods, and static routing. |
| Questions | Wrong/correct answers, cooldown, aliases/case handling, shared progress, revision reset, Unicode limits, flag disclosure, drafts, and restart persistence. |
| Desktop browser | Source selection, field filtering/insertion, autocomplete, diagnostics, execution/cancellation, shortcuts, Vim toggle, sorting/resizing, keyboard cell navigation, JSON inspection/search/copy, safe text rendering, value copy/filter/exclusion/undo, calculated-column filters, timeline filters, and empty results. |
| Hunts | Save/reopen/remove/undo, reload persistence, recent history/clear, clipboard share links, and URL round trip. |
| Mobile browser | 390×844 layout, view switching, keyboard tabs, query execution, result cards, JSON details, value pivots, and horizontal overflow check. |
| Browser failure states | Loading/startup-error screens, malformed API JSON, cancellation, and unavailable/malformed local storage. |
| Supporting UI | Sorting and row identity, tabs, toast expiry/action behavior, browser storage validation and failures. |
| Container | Image build; non-root/read-only runtime; embedded static assets; FTS; persisted challenge/data after restart. |

The completed checks include nine Chromium scenarios and six frontend unit-test files. Go statement coverage without the FTS build tag measured **80.2% overall**, including **91.2% API** and **89.9% KQL**. Full Go tests also pass with the FTS build tag and race detector.

## Bugs fixed during this pass

- `between` emitted SQL that SQLite could not execute; bounds now lower to inclusive comparisons.
- API/manifest text limits counted UTF-8 bytes despite advertising characters; they now count Unicode code points.
- Console logger attributes inherited groups added after those attributes were bound; each attribute now retains its original group scope.
- Null and missing result values violated sort-comparator symmetry; they now compare equally.
- Running a toast action could hide the new toast created by that action; the old toast now closes first.
- Field completion included preceding operators such as `project` in the match; ordinary field tokens now match independently while multiword sort operators remain supported.
- Result filters were inserted before projections/aggregations that defined their columns; filters now append to the producing pipeline.
- README startup descriptions contradicted actual loading/error behavior; the descriptions now match the tested lifecycle.

## Remaining limits

- Browser automation covers Chromium on Linux, with desktop and emulated mobile dimensions. Firefox, Safari, real touch devices, screen readers, and clipboard permission behavior on remote insecure origins were not verified.
- The Docker check targets the local architecture. ARM64 images, registry publishing, Cloudflare tunnels, and external authentication proxies were not exercised.
- The fixtures are small. Production-scale ingestion, sustained load, disk exhaustion, power loss, and all combinations of KQL operators require separate testing.
- The production build still reports its existing JavaScript chunk-size warning.
