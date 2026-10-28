# IPv4 conversion and formatting

Striem's SQLite runtime provides these typed adapters through the independent
KSQL compiler's extension API:

| Function | Arguments | Result | Invalid value |
| --- | --- | --- | --- |
| `parse_ipv4` | IPv4 string | long | null |
| `parse_ipv4_mask` | IPv4 string, integer prefix | long | null |
| `format_ipv4` | IPv4 string or integer, optional prefix | string | empty string |
| `format_ipv4_mask` | IPv4 string or integer, optional prefix | string with CIDR suffix | empty string |
| `ipv4_netmask_suffix` | IPv4 string | long | null |
| `ipv4_compare` | Two IPv4 strings, optional prefix | long (-1, 0 or 1) | null |
| `ipv4_is_match` | Two IPv4 strings, optional prefix | bool | null |
| `ipv4_range_to_cidr_list` | Starting and ending IPv4 strings | dynamic array of CIDR strings | null |

Integers represent network-order unsigned 32-bit address bits in a KQL long.
For example, `parse_ipv4("192.168.1.1")` returns `3232235777`, while the same
address with `/24` returns `3232235776`. Applying a prefix clears host bits.
Prefixes range from 0 through 32; an absent suffix uses 32. An embedded prefix
continues to limit an explicitly supplied prefix, as shown by Microsoft's
formatting examples. `format_ipv4_mask("192.168.1.1/24",32)` therefore returns
`192.168.1.0/24`.

Known input types and argument counts are checked during compilation. Prefixes
can come from columns, scalar bindings or query parameters; they need not be
literal constants. Null prefixes produce null parse results or empty formatting
results. Numeric formatting accepts the range 0–4294967295. Mask calculations
use integer arithmetic, including `/0`, `/32` and the highest address bit.

Comparison and matching apply the smallest prefix length from both addresses
and the optional explicit prefix. Address bits are compared in unsigned numeric
order. Thus `ipv4_compare("10.0.0.1","2.0.0.1")` returns 1, while matching
`192.168.1.1/24` against `192.168.1.255` returns true. A null or invalid explicit
prefix produces null. Boolean metadata survives projections and API JSON output;
invalid addresses produce null rather than false.

Range conversion covers both endpoints of an interval with aligned CIDR blocks
in ascending address order. Each block is maximal within the remaining interval.
The documented interval `1.1.128.0` through `1.1.140.255` yields
`["1.1.128.0/21","1.1.136.0/22","1.1.140.0/24"]`. The full IPv4 space yields
`["0.0.0.0/0"]`; a single address yields one `/32` block. Computation emits at
most 62 blocks and never enumerates individual addresses. Wider arithmetic keeps
the endpoint beyond `255.255.255.255` from wrapping back to zero.

Range endpoints currently require bare dotted-decimal addresses. Invalid/null
endpoints, CIDR endpoint notation and reversed intervals return null. The public
reference specifies valid endpoint conversion but does not settle those invalid
or extended-input cases; these policies need live-engine comparison. Dynamic
metadata allows array indexing, `array_length` and `mv-expand` on the result.

Parsing uses bounded canonical dotted-decimal IPv4 syntax, with an optional
CIDR suffix. IPv6, mapped IPv6, hostnames, abbreviated addresses, leading-zero
octets, whitespace and embedded NUL are rejected. No name lookup is performed.
Strings are bounded by the existing IPv4 address-length policy before parsing;
formatted output is at most 18 ASCII bytes.

SQLite UDF tests cover Microsoft's documented examples, all 33 mask widths,
numeric bounds, nulls, invalid addresses and integer round trips. Compiler/runtime
tests exercise columns, grouping, scalar bindings, arithmetic and result-schema
types. In-process API tests verify long/string/null JSON results, query parameters
and positioned signature errors. The functions are listed in API discovery and
editor completion.

Range-cover tests check the documented output, inclusive endpoints, full-space
and high-bit boundaries, malformed inputs and the 62-block worst case. A fuzz
target verifies alignment, continuity, nonoverlap, maximal block size and exact
endpoint coverage across generated 32-bit intervals.

These are Striem runtime mappings, not new built-in translations in the standalone
KSQL compiler. Its generic feature ledger remains unchanged. Live Kusto
differential certification, all lexical/coercion forms and constant folding remain
open. Other known scalar input types to formatting currently produce an explicit
unsupported-type diagnostic. Microsoft's function catalog declares scalar input
with a TODO to restrict it to string/int/long; behavior for real, bool, datetime
and dynamic overloads needs engine evidence.

The documentation makes the `format_ipv4_mask` prefix optional while the public
function catalog lists it as required. This adapter follows the documented
optional form; that discrepancy still needs live-engine verification.

Primary references:
[parse_ipv4](https://learn.microsoft.com/en-us/kusto/query/parse-ipv4-function?view=microsoft-fabric),
[parse_ipv4_mask](https://learn.microsoft.com/en-us/kusto/query/parse-ipv4-mask-function?view=microsoft-fabric),
[format_ipv4](https://learn.microsoft.com/en-us/kusto/query/format-ipv4-function?view=microsoft-fabric),
[format_ipv4_mask](https://learn.microsoft.com/en-us/kusto/query/format-ipv4-mask-function?view=microsoft-fabric),
[ipv4_netmask_suffix](https://learn.microsoft.com/en-us/kusto/query/ipv4-netmask-suffix-function?view=microsoft-fabric),
[ipv4_compare](https://learn.microsoft.com/en-us/kusto/query/ipv4-compare-function?view=microsoft-fabric),
[ipv4_is_match](https://learn.microsoft.com/en-us/kusto/query/ipv4-is-match-function?view=microsoft-fabric),
[ipv4_range_to_cidr_list](https://learn.microsoft.com/en-us/kusto/query/ipv4-range-to-cidr-list-function?view=microsoft-fabric),
[Microsoft function signatures](https://github.com/microsoft/Kusto-Query-Language/blob/master/src/Kusto.Language/Functions.cs).
