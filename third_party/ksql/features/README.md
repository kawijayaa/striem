# Feature coverage

Coverage has four independent dimensions:

- **Inventory:** every feature in the pinned Microsoft source has a ledger row.
- **Syntax:** a feature is validated, recognized, or preserved as opaque.
- **Translation:** compilation emits SQL or a stable diagnostic.
- **Semantics:** support is exact, equivalent, lossy, or unsupported per target.

Only `exact` and `equivalent` entries claim semantic preservation. `lossy`
translation must be explicitly enabled. `unsupported` features never emit
plausible but incorrect SQL.

The ledger is available through `features.All`, `features.Lookup`, and
`features.JSON`. It is built from the function, aggregate, plugin, operator,
statement, and source catalogs at the upstream commit recorded in
`inventory/source.json`, and is checked against compiler tests.
