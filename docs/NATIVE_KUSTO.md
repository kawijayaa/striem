# Native Kusto API

Striem can optionally forward native KQL to a Kusto service through the
independent KSQL `kusto` package. This adds an API for features executed by that
service, including external data and management scripts. The hunting editor,
its preflight validation and `/api/query` continue to use the local SQLite
compiler. There is no native editor mode or native validation endpoint yet.

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
  "requestId": "KSQL;...",
  "activityId": "..."
}
```

All tables and native column types are retained. JSON numbers remain exact on
the wire; consumers that need exact longs/decimals must use a lossless JSON
decoder rather than converting all numbers to JavaScript `Number`.

Unconfigured execution returns 404; disabled management returns 403. Request
validation errors return 400 or 415. Upstream semantic errors return 400 and
throttling returns 429. Other upstream failures, including authentication,
partial results, protocol errors and resource limits, return 502; request
deadline expiry returns 504. Service errors include correlation IDs, with no
partial tables or raw service error details exposed to the browser.

The native deadline is 30 seconds. Response bytes and decoded table/row/cell
counts are bounded by the KSQL client. Oversize results fail rather than
truncate. Requests are not retried by the application. A management transport
failure can leave its execution outcome uncertain.

## Current evidence and gaps

TLS mock-service tests cover forwarding, typed multi-table results, exact
integers, parameter separation, read-only query properties, management opt-in,
input/origin guards, failure redaction and coexistence with SQLite queries.
Startup tests cover configuration, token rotation and file failures. Live
cluster execution has not been verified.

Native schema/editor integration, service validation, user/delegated identity
flows, progressive streaming, queued/streaming ingestion and upload protocols,
operation polling, command-specific result interpretation and live-service
conformance remain unfinished. The complete KQL implementation goal remains
open; service forwarding does not complete local compiler support.

Service read-only behavior is described in Microsoft's
[request properties](https://learn.microsoft.com/en-us/kusto/api/rest/request-properties?view=azure-data-explorer).
