# Unicode codepoint conversion

Striem registers typed SQLite mappings for `unicode_codepoints_from_string` and
`unicode_codepoints_to_string`, including the deprecated `to_utf8` and
`make_string` aliases. These are application runtime extensions to KSQL, rather
than standalone compiler translations.

The extraction function accepts one string and returns a dynamic array of
Unicode scalar values. For example, `to_utf8("😀")` returns `[128512]`, not the
four UTF-8 byte values or two UTF-16 surrogate code units. The string constructor
accepts 1–64 integer or dynamic-array arguments, flattening argument arrays one
level in argument order:

```kusto
print text=unicode_codepoints_to_string(dynamic([75,117,115]),116,111)
// Kusto
```

Valid scalar values range from 0 through 1,114,111, excluding surrogate values
55,296 through 57,343. Embedded zero characters and supplementary-plane values
are retained. No normalization combines characters or changes their order.
Empty text produces `[]`; an empty array produces an empty string. Invalid
codepoints make the entire constructor result null rather than returning a
partially constructed string or replacement characters.

## Local policies and limits

Array entries currently must be integer JSON tokens. Nulls, strings, nested
arrays, fractions and exponent notation return null; dynamic scalar values are
not accepted in place of an array. Invalid UTF-8 extraction input returns null.
These coercion/null policies need live Kusto differential confirmation. Known
non-integer/non-array constructor arguments and non-string extraction arguments
receive a compile diagnostic.

There is a local 65,536-codepoint budget per evaluation. Extraction input is
also limited to 262,144 UTF-8 bytes; the constructor uses existing dynamic-array
byte and element budgets. Over-budget evaluations raise an execution error
without exposing partial output. The 64-argument signature is documented by
Microsoft; the total-element resource budget is specific to Striem.

Column inputs, scalar bindings, query-defined functions and query parameters
are supported. String/dynamic metadata survives projections, schema queries,
array expansion and JSON API responses. Discovery and editor completion include
the current names and aliases.

Execution tests cover Microsoft's circled-letter, Hebrew, integer, array, mixed
and newline examples, plus aliases, emoji, Unicode boundaries, invalid
surrogates, empty values, all 64 arguments, typed schema and parameterized API
JSON. Runtime boundary tests and round-trip fuzz tests exercise resource limits
and malformed UTF-8. The documentation's numeric scalar `range()` composition example is covered by
the [scalar range execution tests](SCALAR_RANGE.md).
Complete coercion semantics, constant folding and live-engine certification
remain open. The full KQL implementation goal remains incomplete.

Primary references:
[extract codepoints](https://learn.microsoft.com/en-us/kusto/query/unicode-codepoints-from-string-function?view=microsoft-fabric),
[construct text](https://learn.microsoft.com/en-us/kusto/query/unicode-codepoints-to-string-function?view=microsoft-fabric).
