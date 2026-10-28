# IP range lists

Striem registers `ipv4_is_in_any_range` and `ipv6_is_in_any_range` through the
KSQL typed extension API for SQLite execution. Both accept a string host address
(or a dynamic scalar string) followed by either range strings or one dynamic
array of range strings. Results retain bool metadata and serialize as
true/false/null through the query API.

```kusto
let networks = dynamic(["192.168.0.0/16", "10.0.0.0/8"]);
print inside = ipv4_is_in_any_range("192.168.1.6", networks)
```

These functions apply each range's prefix to the host address. A range without a
prefix means an exact address. IPv6 also accepts mapped IPv4 forms using the
normalization described in the [IPv6 guide](IPV6_FUNCTIONS.md). The IPv4 function
accepts comma-separated members inside a range string and trims their whitespace,
consistent with the existing single-range SQLite mapping. IPv6 range strings
each contain one range.

Local validation policy returns null for invalid addresses, candidate addresses
with CIDR suffixes, malformed arrays, null/non-string array members and invalid
ranges. All ranges are validated even after a successful match. An empty array
returns false for a valid host. These edge policies require live Kusto differential
verification; the Microsoft examples do not establish every precedence or
coercion rule. Dynamic range inputs must be arrays, and arrays cannot be mixed
with additional range arguments. Dynamic host inputs must contain a JSON string.

Lists are limited locally to 65,536 ranges and 4 MiB of encoded JSON per
evaluation. Exceeding these budgets raises an execution error. The compiler
groups variadic strings into small SQL calls and combines arrays in a balanced
tree so large lists do not exceed SQLite's per-call argument limit. Existing
array-concatenation budgets and query/request limits also apply. Microsoft’s
128-term threshold concerns index optimization and is not used as a semantic
limit here. SQLite evaluates ranges sequentially; there is no Kusto text index.

Tests execute Microsoft's examples, filtering, typed schema, dynamic host values,
query parameters and true/false/null API output. Both overloads are exercised with
257 ranges. Runtime tests verify the 65,536-item and 4 MiB boundaries and invalid
member precedence. Fuzz checks compare list results with single-range membership.
These are application runtime mappings, not standalone KSQL translations or
evidence of full KQL conformance.

Primary references:
[IPv4 multi-range membership](https://learn.microsoft.com/en-us/kusto/query/ipv4-is-in-any-range-function?view=microsoft-fabric),
[IPv6 multi-range membership](https://learn.microsoft.com/en-us/kusto/query/ipv6-is-in-any-range-function?view=microsoft-fabric).
