# Constant scalar queries

```kusto
let total = toscalar(Events | count);
Events | summarize groupCount=count() by Host
       | extend fraction=1.0*groupCount/total
```

`toscalar()` accepts a scalar expression or a table expression; a table supplies
its first column and first row. Materialized CTEs make reused scalar bindings
constant across rows. SQLite 3.35+ and PostgreSQL 12+ support this lowering;
PostgreSQL is covered by rendering tests, SQLite by execution tests. Other
SQL dialects return a positioned capability diagnostic. Referencing a caller's
row is rejected, including through functions or previously bound tables.
Independent rows within the argument's own pipeline remain available.

The first table column must have a known schema. Empty tables currently return
SQL null; engine differential verification remains open. Optional `kind`
syntax and full Kusto scalar typing remain unfinished. At most 1024 bindings
and 4 MiB of estimated binding SQL work are accepted per compilation; these
are compiler policies, not Kusto limits. Unused lets/function-validation
bindings are removed before rendering and parameter normalization.
