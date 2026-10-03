# Quoted column names

Quoted column declarations use bracketed string names, including compound
strings: `datatable(['some' ' column']:long)[1]`. Output aliases in print,
project, extend, summarize, and project-rename decode the same form and retain
their names in result metadata. Dots inside quoted column names stay part of
the name; `['a.b']` is distinct from the property access `a.b`. SQL identifiers
are quoted by the target dialect. Complete entity-name validation, exact case
resolution, and all projection naming rules remain unfinished.
