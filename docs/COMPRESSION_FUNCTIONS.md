# Compression functions

Striem maps `gzip_compress_to_base64_string`,
`gzip_decompress_from_base64_string`, `zlib_compress_to_base64_string` and
`zlib_decompress_from_base64_string` to its SQLite runtime through KSQL's typed
extension API. Each accepts one string expression and returns a string result.
Columns, scalar bindings and query parameters can supply values. API discovery
and editor completion include all four names.

```kusto
print text = gzip_decompress_from_base64_string(
    gzip_compress_to_base64_string("hello"))
```

Encoders emit deterministic stored DEFLATE blocks, matching the complete bytes
in Microsoft's published gzip and zlib encoding examples. Blocks split at the
RFC 1951 65,535-byte boundary. Stored blocks preserve data without reducing its
size. Gzip uses a zero timestamp, no optional headers, OS 255, CRC32 and an
uncompressed-size trailer. Zlib uses a 15-bit window header and Adler32 trailer.
Other engine versions or inputs may use different compression strategies;
byte-for-byte Kusto encoding equivalence beyond the examples is unverified.

Decoders accept stored and compressed DEFLATE blocks. They validate checksums
and completed streams before returning a value. Gzip requires ID1/ID2/CM and a
zero FLG byte as Microsoft documents; optional header fields are rejected.
Zlib accepts only a 15-bit window and rejects preset dictionaries. UTF-8 text,
including embedded zero bytes, round-trips. Malformed base64, corrupt/truncated
streams, invalid UTF-8, trailing bytes and concatenated streams return null.
An encoded empty string decompresses to an empty string. Exact Kusto behavior
for null inputs, trailing/multiple streams and malformed base64 variants needs
live differential verification; null is the local representation of failed
conversion.

The runtime limits encoder input and decoded output to 4 MiB per evaluation.
Compressed input has a corresponding budget large enough for stored-block
overhead. Over-budget operations raise an execution error and expose no partial
decoded text. Decoding reads at most the output budget plus one byte, so small
compressed inputs cannot cause unrestricted expansion. Base64 follows the
canonical padded standard alphabet policy in the [base64 guide](BASE64_FUNCTIONS.md).
These are local resource limits, not Kusto service limits.

Tests execute every published encoding/decoding example, independently decode
local output with Go's standard library, and decode standard-library compressed
streams. Additional coverage includes empty/Unicode text, block boundaries,
the 4 MiB boundary, corrupt checksums and headers, every truncation of a fixture,
multiple/trailing streams, output-limit enforcement, typed schema and parameterized
API output. Fuzz tests check round trips and malformed streams. Standalone KSQL
translations, dynamic coercions, constant folding and live-engine certification
remain open. This increment does not establish complete KQL implementation.

Primary references:
[gzip encoding](https://learn.microsoft.com/en-us/kusto/query/gzip-base64-compress?view=microsoft-fabric),
[gzip decoding](https://learn.microsoft.com/en-us/kusto/query/gzip-base64-decompress?view=microsoft-fabric),
[zlib encoding](https://learn.microsoft.com/en-us/kusto/query/zlib-base64-compress-function?view=microsoft-fabric),
[zlib decoding](https://learn.microsoft.com/en-us/kusto/query/zlib-base64-decompress-function?view=microsoft-fabric).
