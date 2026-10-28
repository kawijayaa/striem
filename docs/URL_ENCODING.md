# URL encoding

Striem registers typed SQLite mappings for `url_encode`, `url_encode_component`
and `url_decode`. Each accepts one string expression and returns a string.
Known type/arity errors are reported during compilation. Columns, scalar
bindings, query-defined functions and query parameters can supply inputs.
API discovery and editor completion advertise both encoders.

```kusto
print form = url_encode("a b+é"), component = url_encode_component("a b+é")
// form: a+b%2b%c3%a9
// component: a%20b%2b%c3%a9
```

Both encoders percent-escape UTF-8 bytes using lowercase hexadecimal digits.
Form encoding represents spaces with `+`; component encoding uses `%20`.
Literal plus signs become `%2b`. No URL parsing, normalization, DNS or service
request occurs. Trailing slashes and embedded zero bytes are preserved through
escaping. An empty string encodes to an empty string.

The local safe-character set is ASCII letters, digits and `-._!*()`; other
characters are escaped. Microsoft’s documented examples establish the space
and lowercase escape behavior but do not establish the entire punctuation set.
The punctuation policy, null/coercion behavior and alternate malformed forms
still require live Kusto differential testing. The documentation’s component
example also drops a trailing slash in its displayed output; this implementation
preserves the input slash as `%2f` rather than silently removing it.

The existing `url_decode` mapping performs one pass of percent decoding and
preserves literal `+`. Consequently it reverses component encoding but does not
currently reverse form encoding of spaces. That unresolved engine-semantic gap
is explicit here; round-trip tests for the form encoder use an independent form
decoder and do not establish Kusto decoder conformance.

Encoder input and decoder output have a local 65,536-byte budget. Decoder input
allows up to three times that size to accommodate maximum percent expansion.
Invalid UTF-8, malformed percent sequences, non-string runtime values and budget
violations return null. Known argument types are checked before SQL emission.
These are local limits, not Kusto service limits.

Tests cover the Microsoft URL/space examples, Unicode and zero bytes, punctuation
policies, maximum expansion, invalid signatures, schema metadata, function
bindings and parameterized API output. Fuzz tests compare round trips with Go’s
independent form/path decoders. Standalone compiler translations, complete
coercion/null behavior, constant folding and live-engine certification remain
open. These mappings do not establish complete KQL implementation.

Primary references:
[url_encode](https://learn.microsoft.com/en-us/kusto/query/url-encode-function?view=microsoft-fabric),
[url_encode_component](https://learn.microsoft.com/en-us/kusto/query/url-encode-component-function?view=microsoft-fabric),
[url_decode](https://learn.microsoft.com/en-us/kusto/query/url-decode-function?view=microsoft-fabric).
