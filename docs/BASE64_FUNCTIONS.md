# Base64 functions

Striem's SQLite runtime maps the following functions through KSQL's typed
extension API:

| Function | Input | Output |
| --- | --- | --- |
| `base64_encode_tostring` | string | base64 string |
| `base64_encodestring` | string | alias of string encoder |
| `base64_decode_tostring` | base64 string | UTF-8 string |
| `base64_decodestring` | base64 string | alias of string decoder |
| `base64_decode_toarray` | base64 string | dynamic array of unsigned byte values |
| `base64_encode_fromarray` | dynamic integer array | base64 string |

For example, encoding `"Kusto"` produces `"S3VzdG8="`, and decoding that text
to an array produces `[75,117,115,116,111]`. Byte arrays preserve zero bytes and
values through 255. Unlike string decoding, array decoding does not require the
decoded bytes to form valid UTF-8. Empty text and empty arrays round-trip.

Known argument types and arity are checked during compilation. Results retain
string/dynamic metadata through projections, array operations, schema queries
and JSON API responses. Columns, scalar bindings and query parameters can
supply inputs. These remain application runtime mappings, not standalone KSQL
compiler implementations.

## Local policies and remaining conformance work

Decoders currently accept canonical, padded standard base64 only. Whitespace,
missing padding, nonzero padding bits and the URL-safe alphabet are rejected.
String encoding rejects invalid UTF-8. Array encoding accepts integer JSON
tokens from 0 through 255 and rejects nulls, nested arrays, strings, fractions,
exponent notation and out-of-range values. Invalid conversions return null.
The precise Kusto acceptance/coercion behavior for these edge cases needs live
differential verification. In particular, the documentation does not settle all
binary-array and malformed-padding cases; passing round-trip tests is not
evidence of complete engine equivalence.

The local decoded-byte and UTF-8 encoder input budget is 65,536 bytes. Decoders
accept the corresponding padded encoded length (up to 87,384 characters), and
check the decoded length separately. Oversized text or decoded payloads return
null. Array encoding also uses existing dynamic-array budgets: 65,536 elements
and the shared input-byte limit, with explicit errors for exceeded array
budgets. These are application resource limits, not Kusto specification limits.

Tests cover the documented Kusto examples, Unicode, embedded zero/binary bytes,
empty values, malformed inputs, byte/array limits, aliases, array expansion,
typed schema and parameterized API JSON. Fuzz tests check binary and UTF-8
round trips. Full dynamic scalar coercions, constant folding and live-engine
certification remain open.

Primary references:
[string encoding](https://learn.microsoft.com/en-us/kusto/query/base64-encode-tostring-function?view=microsoft-fabric),
[string decoding](https://learn.microsoft.com/en-us/kusto/query/base64-decode-tostring-function?view=microsoft-fabric),
[array decoding](https://learn.microsoft.com/en-us/kusto/query/base64-decode-toarray-function?view=microsoft-fabric),
[array encoding](https://learn.microsoft.com/en-us/kusto/query/base64-encode-fromarray-function?view=microsoft-fabric).
