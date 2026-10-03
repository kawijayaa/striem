# Query-defined functions

## Scalar query-defined functions

```kusto
let adjust = (value:long, offset:long = 2) {
    let increment = () { offset };
    value + increment()
};
print result = adjust(offset=3, value=10)
```

Function calls inline a bound scalar expression and retain its output type.
Arguments resolve in the caller; bodies resolve in their declaration scope.
Parameters and local declarations stay local. Validation also checks unused
function declarations. Scalar parameter types currently supported are bool,
int, long, real/double, string, datetime, and dynamic; numeric widening supports
int to long and int/long to real. Unsupported types,
recursive or unresolved functions, invalid defaults, and invalid argument
contracts produce diagnostics with no SQL.

Declaration/call depth, parameter count, binding work, and expanded SQL work
are bounded. Shared argument trees are counted at their expanded size before
parameter traversal/rendering, preventing exponentially large SQL from small
function definitions. These are compiler resource policies. Full Microsoft
function type coercion, views/stored functions, inline invocation, expression result
naming, and engine conformance remain unfinished.

## Tabular query-defined functions

```kusto
let selectRecent = (T:(x:long), minimum:long = 2) {
    T | where x >= minimum
};
range x from 1 to 4 step 1
| invoke selectRecent()
```

Table-valued results can be used as sources, local bindings, and table
arguments. Zero-parameter functions support both `f` and `f()`. Table
parameters precede scalar parameters and use a declared column schema or
`(*)`. Fixed schemas validate required columns and types while retaining extra
argument columns; wildcard parameters permit star operations but cannot name
input columns during declaration validation. Table expressions such as
`selectRecent((range x from 1 to 4 step 1), 3)` are supported, along with direct
datatable arguments, lexical table capture, and nested function declarations.
`invoke` supplies its input as the first table argument. Both scalar and table
expansion share the resource limits described above. Exact coercion, case
resolution, schema-dependent function analysis and engine differential testing
are still incomplete.
