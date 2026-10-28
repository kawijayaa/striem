# Cryptographic hashes

Striem registers `hash_md5`, `hash_sha1` and `hash_sha256` through KSQL's typed
extension API. Each accepts one represented scalar expression and returns a
lowercase hexadecimal string of 32, 40 or 64 characters respectively. Inputs
may come from columns, scalar bindings, query-defined functions or parameters.
Discovery and completion include all three functions.

String hashing uses the exact UTF-8 bytes, including embedded zero characters.
Datetime hashing first renders UTC text with seven fractional decimal digits,
for example `2020-01-01T00:00:00.0000000Z`. This reproduces Microsoft's published
string and datetime examples for all three algorithms, and avoids hashing a
database-specific timestamp spelling. Datetimes that cannot be represented at
100ns precision, or fall outside years 1–9999 after UTC conversion, raise an
execution error instead of producing an approximate digest.

```kusto
print digest=hash_sha256("World")
// 78ae647dc5544d227130a0682a51e30bc7777fbb6d8a8f17007463a3ecd1d524
```

## Local scalar policies and remaining work

Bool inputs currently hash lowercase `true`/`false`. Int/long inputs hash exact
signed decimal text; real inputs hash Go's shortest round-trip decimal spelling.
Null values hash empty text. These are explicit local policies that require live
Kusto differential confirmation, especially real exponents, signed zero and
nulls. Unknown-type SQLite values follow their represented string/integer/real
type; an unknown Boolean stored as an integer therefore needs an explicit bool
conversion to select Boolean serialization. Invalid UTF-8 returns null.

Dynamic inputs currently receive a compile diagnostic because object/array/scalar
canonical serialization is not established. GUID, decimal and timespan hashing
also remain unfinished. Nonfinite real values and unsupported runtime
representations raise an execution error. Hash input is locally bounded to
4 MiB per evaluation; exceeding that budget raises an error. None of these
policies establishes full scalar coercion conformance.

SQLite execution tests cover every published `World`/datetime example, the
published SHA256 state-name examples, standard digest vectors, empty text,
column inputs, grouping-ready string metadata, scalar function capture, long
precision and local Boolean/null policies. Runtime tests cover Unicode/zero
bytes, timezone normalization, temporal precision/range rejection and resource
boundaries. API tests verify the documented digests from string and datetime
parameters. Standalone KSQL lowering, constant folding, all scalar types and
live-engine certification remain open. Noncryptographic Kusto hash functions
are separate unfinished features.

Primary references:
[hash_md5](https://learn.microsoft.com/en-us/kusto/query/hash-md5-function?view=microsoft-fabric),
[hash_sha1](https://learn.microsoft.com/en-us/kusto/query/hash-sha1-function?view=microsoft-fabric),
[hash_sha256](https://learn.microsoft.com/en-us/kusto/query/hash-sha256-function?view=microsoft-fabric).
