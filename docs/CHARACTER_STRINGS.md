# Character translation and reversal

Striem provides typed SQLite runtime mappings for `translate(searchList,
replacementList, source)` and `reverse(value)`. Results retain string metadata
through projections, schema queries and API JSON; discovery and editor
completion include both names. Columns, scalar functions and query parameters
can supply values.

`translate` performs one pass over the original source, so replacements do not
trigger further replacements. Equal-length lists map by position; a single
replacement character maps every search character to that character; an empty
replacement list deletes matched characters. These forms execute all three
published Microsoft examples. Empty search lists leave the source unchanged.
Duplicate search characters currently use their first occurrence. Unequal
multi-character lists raise an explicit unsupported-case error because their
mapping semantics are not established by the documentation or live evidence.

`reverse` reverses Unicode codepoint order without breaking UTF-8 byte
sequences. It does not reverse grapheme clusters as indivisible units and does
not normalize combining marks. Represented integers, finite reals and Booleans
use the existing scalar text conversion. Datetimes use canonical UTC text with
seven fractional digits before reversal, matching the documented value example.
Temporal inputs beyond 100ns precision or years 1–9999 raise an error.

Translation lists are locally limited to 64 KiB each; sources and outputs have
a 4 MiB budget. Reversal input has a 4 MiB budget. Exceeded budgets raise errors
without partial output. Invalid UTF-8 or unsupported runtime string values
return null. Known invalid signatures/types receive compile diagnostics.

Tests cover every published translation example, alphabet reversal, represented
integer/real/datetime reversal, Unicode/zero bytes, deletion, duplicate policy,
one-pass behavior, scalar function capture, metadata, resource limits and
parameterized API output. Fuzz tests check reversal round trips and compare
translation with a positional reference.

These are application runtime extensions, not standalone KSQL translations.
Duplicate search precedence, supplementary/combining character behavior,
nulls, exact scalar formatting and full coercions need live Kusto differential
verification. Dynamic reversal and timespan reversal remain unsupported, and
the unsupported timespan component still affects the published mixed-type
query; its minute-precision datetime component now executes through the shared
ISO runtime normalizer. Unequal multi-character translation lists, constant folding
and live-service certification remain open. This increment does not establish
complete KQL implementation.

Primary references:
[translate](https://learn.microsoft.com/en-us/kusto/query/translate-function?view=microsoft-fabric),
[reverse](https://learn.microsoft.com/en-us/kusto/query/reverse-function?view=microsoft-fabric).
