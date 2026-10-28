# IPv6 parsing and comparison

Striem's SQLite runtime registers these typed mappings through KSQL's extension
API. They are not standalone compiler translations.

| Function | Arguments | Result | Failed conversion |
| --- | --- | --- | --- |
| `parse_ipv6` | IPv6/IPv4 string | expanded IPv6 string | empty string |
| `parse_ipv6_mask` | IPv6/IPv4 string, integer prefix | expanded IPv6 string | empty string |
| `ipv6_compare` | Two address strings, optional integer prefix | long (-1, 0, 1) | null |
| `ipv6_is_match` | Two address strings, optional integer prefix | bool | null |
| `ipv6_is_in_range` | Host address string, network string | bool | null |

Canonical output contains eight lowercase groups of four hexadecimal digits.
Plain IPv4 inputs map into the IPv4-mapped IPv6 space. For example,
`parse_ipv6("192.168.255.255")` yields
`0000:0000:0000:0000:0000:ffff:c0a8:ffff`. Embedded IPv4 `/24` becomes an IPv6
`/120` and clears the final host byte. The documented `::dotted-IPv4` form also
normalizes to mapped bytes. Native IPv6 addresses retain their 128-bit values.

Masks use integer widths 0 through 128, including widths between byte boundaries.
The effective mask is the minimum of embedded and explicitly supplied prefix
lengths. Explicit prefixes are applied in the 128-bit space. Plain IPv4 suffixes
range from 0 through 32 and are translated by adding 96. For mapped/compatible
dotted-tail shorthand, suffixes through 32 are likewise translated; larger
suffixes use their 128-bit meaning. Pure hexadecimal IPv6 notation uses 128-bit
prefix lengths directly.

Comparison uses unsigned big-endian bytes, not lexical text ordering or floating
point. Both embedded masks and an optional explicit prefix participate. Mixed
IPv4/mapped-IPv6 equality follows Microsoft's examples. Match results retain
Boolean metadata in projections and JSON responses. Invalid addresses or null/
invalid prefixes return null for comparison/matching and empty strings for
parsing.

Input is bounded to 49 bytes before parsing, and output is always 39 ASCII bytes
on success. Zone identifiers, bracketed URI hosts, hostnames, whitespace and
malformed addresses are rejected. No DNS or external service is invoked.
Known argument types and arity are validated before SQL emission. Columns,
scalar lets, query-defined functions and query parameters can supply values.

## Evidence and limits

SQLite tests execute all documented parse/mask examples, the documented mixed
comparison cases, uppercase input, compressed addresses, all 129 mask widths,
high-bit ordering, malformed inputs and null values. Compiled execution tests
cover filtering, scalar functions and Boolean metadata. In-process API tests
verify expanded strings, true/false/null values, parameters, discovery and
signature errors. Fuzz tests check canonical idempotence, unsigned 128-bit
masking and comparison/matching consistency.

Live Kusto differential certification and complete lexical/coercion/constant-
folding semantics remain open. In particular, alternate hexadecimal spellings
of IPv4-compatible addresses, mapped-address suffix interpretation and explicit
short prefixes on IPv4 operands need engine evidence. The policies above
implement the documented examples without asserting full conformance for those
additional forms. Scalar dynamic inputs currently require an explicit string
conversion. Single-range membership applies only the network argument’s prefix, including
non-byte boundaries and unaligned network bases. A range without a prefix
means an exact address. Candidate addresses containing CIDR suffixes return
null; this is a local validation policy requiring live-engine confirmation.
Comma-separated ranges return null for the single-range function.
`ipv6_is_in_any_range` accepts variadic range strings or a single dynamic array;
see [multi-range semantics and limits](IP_RANGE_LISTS.md). The full KQL implementation goal remains incomplete.

Primary references:
[parse_ipv6](https://learn.microsoft.com/en-us/kusto/query/parse-ipv6-function?view=microsoft-fabric),
[parse_ipv6_mask](https://learn.microsoft.com/en-us/kusto/query/parse-ipv6-mask-function?view=microsoft-fabric),
[ipv6_compare](https://learn.microsoft.com/en-us/kusto/query/ipv6-compare-function?view=microsoft-fabric),
[ipv6_is_match](https://learn.microsoft.com/en-us/kusto/query/ipv6-is-match-function?view=microsoft-fabric),
[ipv6_is_in_range](https://learn.microsoft.com/en-us/kusto/query/ipv6-is-in-range-function?view=microsoft-fabric).
