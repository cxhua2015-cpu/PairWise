# Prefix ACL specification

`New` requires `MaxRules > 0` and a default action of `Allow` or `Deny`. Prefixes and addresses use `net/netip`. Every prefix must be valid and canonical (`prefix == prefix.Masked()`). An operation action must be Allow or Deny for Upsert and zero for Delete. Unknown kinds or extra fields return `ErrInvalidInput`. The whole batch is structurally validated before state is read.

Operations execute in input order on an isolated candidate. Upsert inserts or replaces a rule and allocates one consecutive revision starting at 1. Delete requires a currently existing rule, allocates no revision, and returns `ErrNotFound` otherwise. Repeated operations and delete then reinsert are allowed. Final rule count is checked only after the complete batch. Any error rolls back rules, generation and revision allocation.

A successful nonempty batch increments generation once. An empty batch changes nothing. `Result.Revision` is the latest committed revision or zero. `Result.Changed` contains surviving prefixes touched by Upsert, without duplicates, in snapshot order.

`Lookup` rejects an invalid address. It returns the action and canonical prefix from the matching rule with the greatest prefix length. IPv4 rules match only IPv4 addresses and IPv6 rules only IPv6 addresses. With no match it returns the configured default action, an invalid prefix, and `Found=false`.

`Snapshot` sorts IPv4 rules before IPv6 rules, then by masked address ascending, prefix length ascending, and action. All methods are concurrency-safe and return isolated slices.
