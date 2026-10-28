# Datetime construction and addition

Striem registers typed SQLite runtime mappings for `make_datetime` and
`datetime_add`. Both retain datetime metadata through projections, schema queries
and comparisons, and use Striem's fixed-width UTC timestamp storage format.
Columns, scalar bindings, query-defined functions and query parameters can
supply arguments. Discovery and editor completion include both names.

`make_datetime(year, month, day [, hour [, minute [, second]]])` validates the
Gregorian date without normalizing invalid days into another month. Omitted
time components default to zero. Date/hour/minute components must be represented
integers; seconds may be real. Valid seconds are bounded by 59.9999999 and are
rounded to the nearest 100ns tick locally. Invalid or null components return
null. The datetime domain is years 1–9999; the parameter table's mention of
year zero conflicts with that domain and remains a live-engine verification
case. Four-component construction is accepted alongside the documented three-,
five- and six-component forms, consistent with the public catalog’s optional
components. The catalog also includes numeric component coercions and a
one-argument scalar constructor overload; those remain unfinished locally.

`datetime_add(period, amount, datetime)` supports year, quarter, month, week,
day, hour, minute, second, millisecond, microsecond and nanosecond. Period names
are case-insensitive under the local policy. Calendar addition clamps the day
to the last valid day of the target month, preserving the time of day. Fixed
interval addition uses integer ticks with checked wide intermediate arithmetic,
covering years 1–9999 without `time.Duration` overflow. Negative offsets and
dates before the Unix epoch are handled without signed-remainder errors.

```kusto
print next=datetime_add("month",1,make_datetime(2024,1,31))
// 2024-02-29 at midnight UTC
```

Input timestamps are normalized to UTC; addition discards input precision below
100ns. Nanosecond offsets retain only whole ticks, truncating toward zero.
Unknown periods, null/invalid arguments and out-of-range results return null.
These sub-tick, rounding, month-end, period-name and invalid-value policies are
characterized locally and need live differential verification. Argument type/
arity validation is implemented; complete compile-time period-value/provenance
validation remains unfinished.

Tests execute all published constructor examples and all positive/negative
addition examples using `make_datetime` itself. Further coverage includes
calendar clamping, leap dates, fractional seconds, subsecond units, large
offsets, both datetime bounds, negative epoch arithmetic, per-row inputs,
filtering, scalar functions, schema and parameterized API JSON. Calendar fuzz
properties check target month/year and day clamping. Striem stores nine fractional
digits for ordering consistency, with these results aligned to 100ns; Kusto's
published displays use seven fractional digits for the same represented values.

Standalone compiler lowering, all temporal literal/coercion formats, exact
rounding/null/diagnostic behavior, constant folding and live-service certification
remain open. These mappings do not establish full datetime or KQL conformance.

Primary references:
[make_datetime](https://learn.microsoft.com/en-us/kusto/query/make-datetime-function?view=microsoft-fabric),
[datetime_add](https://learn.microsoft.com/en-us/kusto/query/datetime-add-function?view=microsoft-fabric),
[datetime domain and precision](https://learn.microsoft.com/en-us/kusto/query/scalar-data-types/datetime?view=microsoft-fabric).


## Period differences

`datetime_diff(period, left, right)` returns a long count of period boundaries
in `left - right`. Year, quarter and month count calendar index changes. Week
counts Sunday boundaries; day through microsecond count fixed UTC boundaries.
This differs from truncating the elapsed duration: crossing midnight by 100ns
counts as one day. Floor division preserves the same boundary behavior before
the Unix epoch. Case-insensitive period names cover all eleven documented units.

Nanosecond differences use 100ns tick indices. A difference that cannot fit a
signed long raises an explicit execution error without exposing a wrapped or
partial value. All other supported unit differences fit signed long across the
represented datetime domain. Inputs with sub-tick precision are truncated to
the same 100ns representation used by datetime addition. Invalid/null operands
and unknown periods return null under the local policy.

Tests reproduce every documented difference value, including the published
minute-precision datetime forms through the shared ISO runtime normalizer. Additional coverage includes pre-epoch boundary crossings,
Sunday/Monday week boundaries, full-domain microsecond precision, the largest
representable tick-aligned nanosecond difference, overflow rejection, function/
column inputs, long schema metadata and parameterized API output. Fuzz tests
check subtraction symmetry and symmetric nanosecond overflow. Complete legacy literal
formats, invalid-period diagnostic timing, sub-tick/null/coercion semantics and
live-engine certification remain open.

Primary reference:
[datetime_diff](https://learn.microsoft.com/en-us/kusto/query/datetime-diff-function?view=microsoft-fabric).


## Calendar and clock parts

`datetime_part(part, datetime)` and its legacy `datepart` alias return int
metadata, matching Microsoft's function catalog. Supported parts are year,
quarter, month, week_of_year, day, dayOfYear, hour, minute, second, millisecond,
microsecond and nanosecond. Part names use case-insensitive matching locally.
Subsecond values count within the second; nanoseconds are aligned to 100ns.

Single-datetime accessors `getyear`, `getmonth`, `monthofyear`, `dayofmonth`,
`dayofyear`, `hourofday` and `week_of_year` use the same extraction runtime.
ISO weeks start Monday and use the week containing the year's first Thursday;
January dates can therefore belong to week 52/53 of the previous ISO year.
The obsolete `weekofyear` function/part uses a different first-Wednesday rule
and remains unsupported. It is not treated as an alias for ISO week numbering.
`dayofweek` returns a timespan in Kusto and also remains outside these integer
accessors pending proper timespan typing.

Invalid/null dates and unknown or unsupported parts return null under the local
policy. Type/arity validation is implemented; literal-only part provenance,
exact invalid-part diagnostic timing and full temporal coercions remain open.
Column expressions, scalar functions and query parameters retain int metadata
through schema queries and API results. Tests cover every published part value,
all four published ISO week examples, leap years, ISO year transitions, UTC
normalization, date bounds, schema and parameterized API output. Fuzz properties
reconstruct datetimes from extracted components. Live-engine certification and
constant folding remain unfinished.

Primary references:
[datetime_part](https://learn.microsoft.com/en-us/kusto/query/datetime-part-function?view=microsoft-fabric),
[ISO week accessor](https://learn.microsoft.com/en-us/kusto/query/week-of-year-function?view=microsoft-fabric),
[Microsoft function catalog](https://github.com/microsoft/Kusto-Query-Language/blob/master/src/Kusto.Language/Functions.cs).


## Unix timestamp conversions

`unixtime_seconds_todatetime`, `unixtime_milliseconds_todatetime`,
`unixtime_microseconds_todatetime` and `unixtime_nanoseconds_todatetime` accept
one represented numeric expression and return datetime metadata. Integer values
are scaled with exact rational arithmetic, preserving long microsecond and
nanosecond digits beyond binary64's integer precision. Negative timestamps are
normalized before formatting, including values immediately before the epoch.
The local output format remains fixed-width UTC with nine fractional digits,
aligned to 100ns ticks.

Finite real inputs use their shortest decimal round-trip spelling for rational
scaling. Fractional ticks are truncated toward zero. This avoids an additional
binary64 multiplication in the local runtime, but exact Kusto real quantization
and sub-tick behavior need live comparison. Decimal inputs and complete numeric
coercion semantics remain unfinished. Null, unsupported/nonfinite inputs and
results outside years 1–9999 return null.

Tests execute the published 2019-01-01 examples for all four units, exact
subsecond long values, negative/fractional inputs, datetime bounds, nulls,
function/column usage, schema metadata and parameterized API output. Integer
fuzz tests compare all unit conversions with independent standard-library Unix
construction across the signed-long nanosecond interval. Standalone lowering,
constant folding and live-service certification remain open.

Primary references:
[seconds](https://learn.microsoft.com/en-us/kusto/query/unixtime-seconds-todatetime-function?view=microsoft-fabric),
[milliseconds](https://learn.microsoft.com/en-us/kusto/query/unixtime-milliseconds-todatetime-function?view=microsoft-fabric),
[microseconds](https://learn.microsoft.com/en-us/kusto/query/unixtime-microseconds-todatetime-function?view=microsoft-fabric),
[nanoseconds](https://learn.microsoft.com/en-us/kusto/query/unixtime-nanoseconds-todatetime-function?view=microsoft-fabric).


## ISO minute precision and UTC bounds

The shared `todatetime` runtime normalizer accepts ISO dates and date/time values
with either minute or second precision. Time may use `T` or a space; minute-only
forms insert zero seconds before parsing, including `Z` and numeric-offset forms.
Inputs are bounded to the longest supported ISO spelling before regex/parsing.
Timezone offset hours/minutes are checked against their lexical ranges. UTC
normalization must remain in years 1–9999; year-zero input and offsets crossing
outside the datetime domain return null instead of producing invalid stored
strings.

Tests execute Microsoft's minute-precision ISO examples, the original
`datetime_diff` minute forms, minute-only literal reversal and datetime addition,
column conversion, UTC bounds and string-parameter API conversion. Fuzz tests
check accepted results for canonical idempotence and valid UTC range. Earlier
notes incorrectly attributed the literal failure to the companion parser; the
failure on that path was in Striem's runtime normalization.

The companion compiler's separate typed datetime query-parameter parser still
accepts fewer formats and does not yet accept these minute-only inputs. String
parameters explicitly passed through `todatetime` exercise the runtime path;
this does not repair typed parameter binding or default-value conversion.
RFC 822/850 and other legacy formats, complete timezone policies and exact
100ns conversion/rounding behavior remain open. Existing nanosecond storage
precision has not been changed by this increment.

Primary reference:
[datetime formats and domain](https://learn.microsoft.com/en-us/kusto/query/scalar-data-types/datetime?view=microsoft-fabric).
