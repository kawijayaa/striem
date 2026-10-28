# Bitwise scalar functions

Striem registers typed SQLite mappings for `binary_and`, `binary_or`,
`binary_xor`, `binary_not`, `binary_shift_left`, `binary_shift_right` and
`bitset_count_ones`. Inputs are represented int/long values and results have
long metadata, including population counts. These are application runtime
extensions to KSQL, not standalone compiler translations.

AND, OR, XOR and complement operate on signed 64-bit two's-complement values.
Shift counts are reduced modulo 64 after rejecting negative counts. Left shift
discards bits beyond the 64-bit width; right shift propagates the sign bit.
Population count examines all 64 bits, so `bitset_count_ones(-1)` is 64 and the
minimum signed long has one set bit. No floating-point conversion occurs.

```kusto
print mask=binary_and(6,3), shifted=binary_shift_left(1,65),
      ones=bitset_count_ones(42)
// 2, 2, 3
```

Known wrong arity/types receive compile diagnostics. Nulls and unsupported
runtime representations return null; negative shifts return null. Columns,
scalar bindings, query-defined functions and query parameters are supported.
API JSON preserves exact long digits and nulls, while discovery and completion
include all seven names.

Tests cover the published examples, every shift position, shifts exceeding 64,
the maximum signed shift count, zero/high-bit/all-one values, nulls, negative
shifts, filtering, scalar functions, schema metadata and parameterized API JSON.
Fuzz properties check De Morgan identities, complementary population counts
and individual shifted-bit placement.

Complete dynamic/coercion/null behavior, constant folding and live-engine
certification remain open. In particular, arithmetic right shift for negative
operands and sign extension of negative int inputs are explicit local policies
that require live Kusto differential confirmation beyond the positive examples.
The aggregate and hexadecimal mappings below share these conformance limits. This increment
does not establish complete KQL implementation.

Primary references:
[AND](https://learn.microsoft.com/en-us/kusto/query/binary-and-function?view=microsoft-fabric),
[OR](https://learn.microsoft.com/en-us/kusto/query/binary-or-function?view=microsoft-fabric),
[XOR](https://learn.microsoft.com/en-us/kusto/query/binary-xor-function?view=microsoft-fabric),
[complement](https://learn.microsoft.com/en-us/kusto/query/binary-not-function?view=microsoft-fabric),
[left shift](https://learn.microsoft.com/en-us/kusto/query/binary-shift-left-function?view=microsoft-fabric),
[right shift](https://learn.microsoft.com/en-us/kusto/query/binary-shift-right-function?view=microsoft-fabric),
[population count](https://learn.microsoft.com/en-us/kusto/query/bitset-count-ones-function?view=microsoft-fabric).


## Bitwise aggregates and hexadecimal output

`binary_all_and`, `binary_all_or` and `binary_all_xor` accept one represented
integer expression in an aggregate context and return a long. Each group has
independent constant-size accumulator state. Nulls are skipped; all-null and
ungrouped empty inputs return null, following the general summarize default
for aggregates without another specified default. Grouped empty input produces
no rows. Known wrong types, nested aggregates and use outside an aggregate
context receive compile diagnostics; unexpected non-integer runtime values
raise an execution error rather than silently participating.

`tohex(value [, minLength])` returns lowercase hexadecimal text. It preserves
32-bit negative int and 64-bit negative long representations through the input
type metadata. Optional widths pad with zeroes and clamp to 16; longer natural
representations are retained. The default minimum width is one. Nulls and widths
below one return null under the local policy. Negative int padding beyond eight
digits, invalid-width/null precedence and broader coercion behavior still need
live-engine confirmation. An unknown-type negative SQLite integer uses long
formatting; explicit int conversion is required for 32-bit formatting.

The aggregate tests reproduce Microsoft's `CAFEF00D` results using the exact
decimal equivalents of the documented inputs. The companion compiler currently
misclassifies hexadecimal literals as interval literals, emitting SQL such as
`INTERVAL '0xFFFFFFFF'`. Therefore the published hexadecimal-literal queries do
not yet execute verbatim. This is an unresolved compiler defect, independent
of the aggregate calculation, and cannot be repaired by these runtime mappings.

Tests also exercise group isolation, duplicate-sensitive XOR, null/empty groups,
all published `tohex` comparisons, width clamping, signed integer boundaries,
aggregate scope diagnostics, output schema and parameterized API exact long
results. Fuzz tests compare partitioned and reordered aggregation. Full compiler
literal support, coercion and live-service certification remain open.

Primary references:
[aggregate AND](https://learn.microsoft.com/en-us/kusto/query/binary-all-and-aggregation-function?view=microsoft-fabric),
[aggregate OR](https://learn.microsoft.com/en-us/kusto/query/binary-all-or-aggregation-function?view=microsoft-fabric),
[aggregate XOR](https://learn.microsoft.com/en-us/kusto/query/binary-all-xor-aggregation-function?view=microsoft-fabric),
[hexadecimal conversion](https://learn.microsoft.com/en-us/kusto/query/tohex-function?view=microsoft-fabric),
[summarize defaults and nulls](https://learn.microsoft.com/en-us/kusto/query/summarize-operator?view=microsoft-fabric).
