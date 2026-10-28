# Scalar range arrays

Striem's SQLite runtime implements numeric `range(start, stop [, step])` through
KSQL's typed extension API. It produces a dynamic array, distinct from the
tabular `range name from ... to ... step ...` source.

```kusto
print r=range(1,8,3)
// [1,4,7]
```

The default numeric step is 1. Endpoints are inclusive when reached. Negative
steps generate descending sequences; a step pointing away from the stop produces
an empty array. Integer-only inputs retain signed 64-bit precision and stop
before arithmetic overflow. Mixed real/integer inputs use binary64 arithmetic.
Arrays truncate at Microsoft's documented 1,048,576-element maximum.

Range arguments can reference input columns or scalar/query-parameter bindings.
Results retain dynamic metadata and can feed SQLite array expansion. The
published `range(1,1000000000) | mv-expand | count` example returns 1,048,576.
Composition with the Unicode constructor executes Microsoft's digit/uppercase/
lowercase example using three range arrays.

## Remaining semantics and integration

Datetime and timespan arrays remain unsupported with a compile diagnostic.
The companion compiler currently misclassifies a bare `range(...)` at the start
of a let binding or query-defined function body as a tabular range source, for example
`let r=range(1,3);` or `let f=(n:long){range(1,n)};`. That parser ambiguity requires a compiler fix.
Nested scalar usage such as `iff(true,range(1,n),dynamic([]))` works and is
tested; it does not resolve the bare-body grammar defect.

Zero steps and null/non-numeric runtime inputs currently return null. Known
non-numeric argument types are rejected during compilation. Nonfinite real
inputs and steps too small to advance a binary64 value raise an execution
error without returning a partial array. Real sequences use repeated addition;
exact Kusto behavior for accumulated rounding, stagnation, zero steps, nulls
and descending edge cases requires live differential verification. Mixed
integer/real inputs can lose integer precision through binary64 promotion.
Dynamic numeric subtype tags are not preserved by JSON itself.

Other array consumers retain their independent resource budgets. Producing a
million-element range does not bypass a consumer's smaller array byte/element
limit. In particular, this increment does not certify every large-array
composition. Each range evaluation allocates its final JSON array; the
documented element cap bounds execution but does not make it lazy.

Tests cover the published numeric and truncation examples, per-row bounds,
exact long endpoints, overflow protection, descending and fractional sequences,
empty/null policies, schema, Unicode construction and parameterized API JSON.
Integer fuzz properties check progression, order and endpoints. Temporal arrays,
the bare-body parser fix, constant folding, complete numeric/null semantics
and live-engine certification remain open. These are application runtime
mappings, not complete standalone KSQL range support or full KQL implementation.

Primary reference:
[range function](https://learn.microsoft.com/en-us/kusto/query/range-function?view=microsoft-fabric).
