# readyqueue290

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

## Design notes

### Index

Items live in a single `map[string]Item` keyed by ID, giving O(1) existence
checks for `Enqueue`/`Cancel`. Canonical order (Priority desc, ReadyAt asc, ID
asc) is produced on demand by sorting in `Pop`/`Snapshot`, so there is no
secondary index to keep consistent under failure rollback.

### Candidate transaction

`Apply` first runs the shared structural precheck (`validateBatchStructure`,
also used by `ValidateBatch`) without touching state, then verifies monotonic
time, then executes all ops against a private candidate map with a local
revision counter. Final capacity is checked only at the end. The candidate,
clock, generation, and revision are committed to the queue only when every
step succeeds; any error (`ErrExists`, `ErrNotFound`, `ErrCapacity`, …)
discards the candidate, so time, state, and revision roll back for free.

### Ownership

All mutable state is guarded by one `sync.RWMutex`; writers (`Apply`, `Pop`)
take the write lock, readers (`Snapshot`, `Stats`, `Clone`) the read lock,
making every public method linearizable. Returned slices/maps are freshly
allocated, and `Clone` deep-copies the item map and logical clocks
(`now`, `generation`, `nextRevision`) into an independent queue — no aliasing
between clone and original.

### Complexity

- `Apply`: O(n + k) for a batch of k ops over n stored items (candidate copy + op replay).
- `Pop`: O(n + r log r) for r ready items (scan + sort + delete).
- `Snapshot`/`Clone`: O(n log n) / O(n).
- `Stats`/`ValidateBatch`: O(1) / O(k).
