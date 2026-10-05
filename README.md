# prefixacl

A concurrency-safe, in-memory prefix ACL table for edge proxies. Go 1.22+, standard library only. See `SPEC.md` for the full contract.

## Design

### Index

Rules live in a single `map[netip.Prefix]Rule` guarded by a `sync.RWMutex`. Canonical prefixes (`p == p.Masked()`) are used as map keys, so IPv4 and IPv6 rules coexist in one map and deduplication is free. The table also tracks a monotonically increasing `generation` (bumped once per successful nonempty batch) and a `nextRevision` counter (one consecutive revision per Upsert, starting at 1).

### Candidate transactions

`Apply` runs in three phases under the write lock:

1. **Structural validation** of every op (kind, canonical prefix, action rules) before any state is read.
2. **Execution** on an isolated candidate: the live map is copied, ops are applied in input order, and revisions are allocated from a local counter.
3. **Commit**: the final rule count is checked against `MaxRules` only after the whole batch; on success the candidate map, generation, and revision counter are swapped in.

Any error (`ErrInvalidInput`, `ErrNotFound`, `ErrCapacity`) discards the candidate, so rules, generation, and revision allocation are fully rolled back. `Result.Changed` reports surviving upserted prefixes, deduplicated, in snapshot order.

### Matching

`Lookup` scans rules of the same address family (IPv4 rules match only IPv4 addresses and vice versa) and picks the matching rule with the greatest prefix length (longest-prefix match). With no match it returns the configured default action, an invalid prefix, and `Found=false`.

### Snapshot ordering

`Snapshot` sorts deterministically: IPv4 before IPv6, then masked address ascending, prefix length ascending, then action. All returned slices are freshly allocated and isolated from table state.

### Complexity

- `Apply`: O(n + r) for n ops and r existing rules (map copy per batch).
- `Lookup`: O(r) scan; suitable for edge-proxy rule counts.
- `Snapshot`: O(r log r) due to sorting.
- Space: O(r).

All methods are safe for concurrent use; readers (`Lookup`, `Snapshot`) share a read lock, writers (`Apply`) take the write lock.
