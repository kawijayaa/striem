# Native Kusto API

Striem can optionally forward native KQL to a Kusto service through the
independent KSQL `kusto` package. This adds an API for features executed by that
service, including external data and management scripts. The hunting editor,
its preflight validation and `/api/query` continue to use the local SQLite
compiler. A separate [Kusto service console](/kusto.html) runs native queries
and management commands without local compiler preflight. Native schema-aware
completion and a native validation endpoint remain open.

## Configuration

Set all three required variables:

```sh
STRIEM_KUSTO_ENDPOINT=https://my-cluster.region.kusto.windows.net
STRIEM_KUSTO_DATABASE=Telemetry
STRIEM_KUSTO_TOKEN_FILE=/run/secrets/kusto-access-token
```

The token file contains a bearer access token for that cluster. Striem reads it
at startup and for every native request, allowing an external credential manager
to replace it atomically. Striem does not acquire or refresh tokens itself.
Files must be regular, nonempty and at most 64 KiB. Credentials never come from
browser request fields. Invalid or incomplete configuration fails startup;
HTTPS is required and redirects are refused.

`STRIEM_KUSTO_MANAGEMENT=true` additionally enables management execution. It is
disabled by default. This permits mutations, scripts, ingest and export commands
authorized by the configured principal. Native routes have the same origin
checks as other Striem POST routes; those checks are not user authentication.
Deployments must restrict access to these routes according to that principal's
permissions, for example through their authenticated reverse proxy.

## Requests

`POST /api/kusto/query` accepts:

```json
{
  "query": "declare query_parameters(host:string); Events | where Host == host",
  "parameters": {"host": "server-01"}
}
```

`POST /api/kusto/management` accepts the same shape, with management command text
in `query`. Both require `Content-Type: application/json` and
`X-Striem-Request: 1`. The configured database is used for every request.
Unknown request fields, empty text, text above 32 KiB, bodies above 64 KiB and
trailing JSON are rejected before contacting Kusto.

Native queries also accept optional `"progressive": true`. This requests Kusto's
progressive wire format. Striem reconstructs the final tables and sends a single
JSON response after all completion records pass. Append and replacement fragments
can interleave across tables. Management requests reject progressive mode.
If supplied, the progressive flag must be a Boolean; parameter values must be
strings. Explicit null flags and null parameter values are rejected.

Query requests set the service's `request_readonly` property; callers cannot
override it through request options. Management requests have no such restriction
once explicitly enabled. Cross-database/cluster references and external services
inside KQL remain subject to the service's authorization and policies.

## Responses and errors

Successful responses contain `tables`, `requestId` and `activityId`:

```json
{
  "tables": [{
    "id": 0,
    "kind": "PrimaryResult",
    "name": "Results",
    "columns": [{"name": "Count", "type": "long"}],
    "rows": [[9223372036854775807]]
  }],
  "progressive": false,
  "requestId": "KSQL;...",
  "activityId": "..."
}
```

All tables and native column types are retained. JSON numbers remain exact on
the wire; consumers that need exact longs/decimals must use a lossless JSON
decoder rather than converting all numbers to JavaScript `Number`. The response
`progressive` flag records the service's wire format. If progress frames appeared,
each table includes its last `progress` percentage. Successful completion does
not require that estimate to reach 100; completion records determine success.

Unconfigured execution returns 404; disabled management returns 403. Request
validation errors return 400 or 415. Upstream semantic errors return 400 and
throttling returns 429. Other upstream failures, including authentication,
partial results, protocol errors and resource limits, return 502; request
deadline expiry returns 504. Service errors include correlation IDs, with no
partial tables or raw service error details exposed to the browser.

The native deadline is 30 seconds. Response bytes and decoded table/row/cell
counts and v2 frame counts are bounded by the KSQL client, including rows/cells
evicted by replacement fragments. Oversize results fail rather than
truncate. Requests are not retried by the application. A management transport
failure can leave its execution outcome uncertain.

## Current evidence and gaps

TLS mock-service tests cover forwarding, typed multi-table results, exact
integers, parameter separation, read-only query properties, management opt-in,
input/origin guards, failure redaction and coexistence with SQLite queries.
Progressive tests cover replacement, exact values, progress metadata, read-only
properties, malformed completion, partial failure and management rejection.
Startup tests cover configuration, token rotation and file failures. Live
cluster execution has not been verified.

Native schema/editor integration, service validation, user/delegated identity
flows, public network streaming/progress delivery, queued/streaming ingestion and upload protocols,
operation polling, command-specific result interpretation and live-service
conformance remain unfinished. The complete KQL implementation goal remains
open; service forwarding does not complete local compiler support.

Service read-only behavior is described in Microsoft's
[request properties](https://learn.microsoft.com/en-us/kusto/api/rest/request-properties?view=azure-data-explorer).

## Browser console

Open `/kusto.html` from the workspace’s **Kusto service** link. The console reads
`GET /api/kusto/capabilities`, which returns only `enabled` and `management`
Booleans with `Cache-Control: no-store`. It exposes no endpoint, database or
credential fields. Execution stays disabled if the service is unavailable or
configuration cannot be read. Management is a separate, explicit execution mode
and remains unavailable unless the server enables it.

The console accepts native parameter JSON with string values, optional progressive
query results, cancellation and response download. It shows the complete raw JSON
response, including every table and column type, without converting native numeric
cells to browser numbers. Long/decimal digits therefore remain intact in display
and download. It does not save query text, parameters or responses to browser
storage. Canceling aborts the browser request; it does not prove remote execution
has stopped, particularly for management commands.

Typed request-unit tests verify exact numeric response text, parameter encoding,
mode selection, request size limits, errors and cancellation. In-process Go tests
verify capability filtering and the built static page. Browser scenarios cover
query/management selection, exact result display and download, but have not been
executed in the current socket-restricted environment. There is no live-service
or visual-browser certification. Tabular browsing, native schema completion and
service validation remain unfinished.
