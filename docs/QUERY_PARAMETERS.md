# Local query parameters

`POST /api/query` and `POST /api/query/validate` accept an optional `parameters`
object alongside the query:

```json
{
  "query": "declare query_parameters(threshold:long=90, host:string); Events | where Score > threshold and Host == host",
  "parameters": {"threshold": 100, "host": "server-01"}
}
```

Local parameters are JSON numbers, Booleans, strings, nulls, objects or arrays
according to the declared type. They are not strings encoding KQL expressions.
The [native Kusto API](NATIVE_KUSTO.md) uses the service's separate string-encoded
parameter format. Exact long values remain JSON numbers on the wire; clients
must use a lossless decoder when integers exceed JavaScript's precise range.

Declared bool/int/long/real/string/datetime/dynamic types and common aliases are
supported. Int and long enforce signed bounds; real parameters must be finite.
String null is rejected. Dynamic parameters preserve JSON structure and cannot
have defaults. Date inputs accept common ISO formats and normalize through
Striem's datetime runtime. Scalar literal defaults work in the ordinary editor
without a parameters object, and the editor suggests the declaration statement.
The editor has no parameter-value controls or declared-symbol completion yet.

Both execution and preflight validation use the same immutable typed bindings.
Values use SQL placeholders, and defaults cannot execute function extensions.
Missing values, type/overflow errors and invalid defaults return positioned 400
responses. Malformed parameter objects and duplicate names also return 400.
Error messages omit parameter values. Existing request/query size, execution
deadline and result bounds still apply.
Invalid UTF-8 and requests exceeding 64 KiB are rejected before JSON decoding,
so malformed input cannot silently change a parameter's text.

Tests execute defaults/overrides, exact long/dynamic values, Boolean results,
SQL-like strings, date reuse and validation. GUID/decimal/timespan types,
nonfinite reals, all Kusto date/coercion/type-tag/name rules, nested declaration
scopes and live-engine conformance remain open. The full KQL goal is incomplete.
Constant-only adapters, including `extract_all` regex patterns, still need
compiler value metadata and currently reject parameter-backed arguments.

The current working increment requires the companion compiler changes recorded
in [implementation status](KQL_IMPLEMENTATION.md). CI pinning and publication
remain pending. Current sandbox restrictions prevent TCP-based test servers and
browser verification; in-process API and compiler tests cover the implemented
binding paths without network sockets.
