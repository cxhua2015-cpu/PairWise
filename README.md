# expirytable224

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

## Design notes

### Index

Entries live in a single `map[string]Entry` keyed by the entry key, guarded by one
`sync.Mutex`. Point lookups (Put/Touch/Delete) are O(1); expiry scans are O(n)
over live entries. A single mutex keeps every public method (`Apply`, `Expire`,
`Snapshot`, `Stats`, `Clone`, `ValidateBatch`) linearizable and safe for
concurrent callers.

### Candidate transactions

`Apply` first runs the shared structural validation (`validation.go`), then
checks monotonic time. It builds a candidate map from the committed state,
evicts entries with `ExpiresAt <= Now` (closed boundary), and replays the ops
in order, allocating one revision per Put/Touch. The candidate is committed
only if every op succeeds and the final size fits `MaxEntries`; any error
(`ErrNotFound`, `ErrCapacity`, ...) discards the candidate, so evictions, the
clock, the revision counter and the generation roll back together. A
non-empty successful batch bumps `Generation` exactly once; an empty batch is
a no-op. `Expire` uses the same closed `ExpiresAt <= now` boundary.

### Ownership

All returned values own their memory: `Snapshot` and `Expire` hand out freshly
allocated, key-sorted slices, and `Clone` copies the entry map plus the
logical clocks (`now`, `generation`, `revision`) so the clone shares nothing
with the original and diverges independently.

### Complexity

- `Apply`: O(n + m) for n live entries (candidate copy + eviction) and m ops.
- `Expire` / `Snapshot` / `Clone`: O(n) (plus O(n log n) sorting for slices).
- `Stats`: O(1). `ValidateBatch`: O(m), no state access.
