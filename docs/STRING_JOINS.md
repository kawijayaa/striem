# Array joining and string replication

Striem provides typed SQLite runtime mappings for `strcat_array(array, delimiter)`,
`strcat_delim(delimiter, argument1, argument2, ...)`, and
`strrep(value, multiplier [, delimiter])`. All return string metadata and
support column expressions, scalar functions and query parameters. Discovery and
editor completion include all three names.

`strcat_array` joins dynamic array elements with a string delimiter. The published
numeric and quote-delimiter examples execute unchanged. JSON strings lose their
JSON quotes; numeric tokens retain exact digits, Booleans use lowercase text,
null entries contribute empty text, and structured values use compact JSON.
Empty arrays produce an empty string. These element-formatting/null policies
need live Kusto verification beyond the documented numeric examples, especially
real exponent normalization, nested objects and dynamic type distinctions.

```kusto
print text=strcat_array(dynamic([1,2,3]), "->")
// 1->2->3
```

`strrep` joins repeated copies of its source with an optional delimiter, which
defaults to empty text. Represented integer, finite real and Boolean source/
delimiter values use the existing scalar text conversion. String and integer
examples from Microsoft execute correctly. Typed datetimes use canonical UTC
text with seven fractional digits; timespan/dynamic coercions remain unsupported. Null source/delimiter values return null under the local policy.

The Microsoft `strrep` page gives conflicting multiplier limits: the parameter
table allows 1–67,108,864, while the return section caps repetition at 1,024.
This mapping accepts the parameter-table range and clamps repetition to 1,024.
Zero, negative, null and above-range multipliers return null. This policy is
characterized by tests and still requires live engine verification; it is not
a claim that the documentation conflict has been resolved.

These functions limit individual string inputs and final output to 4 MiB.
Replication calculates its final size with bounded arithmetic before allocating;
array joining checks output growth before each append. Exceeding a budget raises
an execution error without partial text. Array inputs also use the shared
dynamic-array byte and 65,536-element limits. Invalid UTF-8 and malformed/
non-array runtime values return null. Known wrong types/arity receive compile
diagnostics.

Tests cover documented numeric/delimiter/replication examples, Unicode/zero bytes,
exact long tokens, null/empty policies, column/function inputs, scalar range
composition, schema metadata, count caps, output limits and parameterized API
JSON. Fuzz tests compare array joining and replication with an independent
string join. Complete coercion/formatting/null semantics, temporal examples,
the multiplier-limit conflict, constant folding, standalone KSQL lowering and
live-service certification remain open. The full KQL goal remains incomplete.

Primary references:
[strcat_array](https://learn.microsoft.com/en-us/kusto/query/strcat-array-function?view=microsoft-fabric),
[strrep](https://learn.microsoft.com/en-us/kusto/query/strrep-function?view=microsoft-fabric).


## Scalar delimiter joining

`strcat_delim` takes a string delimiter followed by 2–64 scalar values. Supported
represented scalar values are strings, integers, finite reals, Booleans and
datetimes. Null values contribute empty fields, preserving their surrounding
delimiters. Dynamic values and timespans remain unsupported with the compiler's
current type representation; the published example containing `1s` therefore
does not execute verbatim. A non-temporal version exercises numeric/string/
Boolean conversion, while separate tests cover datetime conversion and all
64 scalar arguments.

The delimiter join uses bounded incremental appends rather than materializing
an intermediate dynamic array. Its input conversions share the canonical UTC
formatter used by datetime hashing and reversal. Datetimes outside years 1–9999
or beyond 100ns precision raise errors. Exact real, null and temporal coercion
semantics still require live engine verification. Tests cover resource errors,
null fields, Unicode, zero-byte delimiters, argument/type limits, schema and
parameterized API output with exact long digits.

Primary reference:
[strcat_delim](https://learn.microsoft.com/en-us/kusto/query/strcat-delim-function?view=microsoft-fabric).
