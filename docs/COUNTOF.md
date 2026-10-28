# Substring and regex counting

Striem maps `countof(source, search [, kind])` to its SQLite runtime through
KSQL's typed extension API. The default kind is `normal`; `regex` selects regular
expression matching. Both modes return long metadata, including zero matches.
Columns, scalar bindings, query-defined functions and query parameters can
supply source/search values. Discovery and editor completion include the name.

```kusto
print plain=countof("ababa","aba"), regex=countof("ababa","aba","regex")
// 2, 1
```

Plain text matches can overlap, and are case-sensitive. A prefix-table scan
counts them in time linear in the combined source and search byte lengths,
avoiding repeated rescans of long repeated patterns. Inputs must be valid
UTF-8; a valid search string cannot begin inside a multibyte codepoint.

Regex matches are nonoverlapping and use the existing bounded compiled-pattern
cache. Matching runs against the complete source, preserving `^` and `$`
semantics. The runtime uses Go's regexp implementation; it does not certify
all Kusto regex syntax or matching behavior. In particular, zero-width matches
and adjacent empty/nonempty matches follow Go's rules and need live engine
comparison. Empty plain search currently returns zero under the local policy;
empty regex follows the regex engine's boundary matching behavior.

Plain source input is locally limited to 4 MiB and search text to 64 KiB. Regex
mode limits source to 64 KiB and pattern to 2 KiB. The regex source bound also
bounds allocation to at most 65,537 match-index pairs. Exceeded budgets, invalid
regex and unsupported kind values raise execution errors without partial counts.
Null, non-string runtime values and invalid UTF-8 return null. Known wrong
argument types/arity receive compile diagnostics.

Microsoft's function catalog restricts kind to a literal `normal` or `regex`.
This extension checks visible literal values and validates all kinds at runtime,
but does not yet enforce literal-only provenance for bound/parameterized
expressions. Constant metadata in the companion compiler is needed for complete
compile-time validation. Exact empty-search/null/coercion semantics, Unicode
regex behavior and diagnostic timing remain open.

Tests execute all six published examples, column filtering, scalar functions,
schema metadata, Unicode and embedded zero bytes, anchors, overlap-heavy input,
resource limits, invalid patterns and parameterized API output. Fuzz properties
compare normal matching with an independent prefix-at-character-boundary count.
Standalone compiler translations, constant folding and live Kusto certification
remain unfinished. This increment does not establish full KQL conformance.

Primary references:
[countof](https://learn.microsoft.com/en-us/kusto/query/countof-function?view=microsoft-fabric),
[Microsoft function signatures](https://github.com/microsoft/Kusto-Query-Language/blob/master/src/Kusto.Language/Functions.cs).
