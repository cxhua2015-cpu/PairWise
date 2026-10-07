# readyqueue405

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

## Design notes

### Index

Items live in a single `map[string]Item` keyed by ID, giving O(1) enqueue/cancel membership checks. There is no persistent heap: `Pop` and `Snapshot` collect matching entries and sort them on demand by the canonical order (Priority desc, ReadyAt asc, ID asc). This keeps the transaction path simple and makes rollback trivial.

### Candidate transaction

`Apply` first runs the shared structural precheck (`ValidateBatch`, no state access), then verifies monotonic time under the lock. For non-empty batches it copies the item map into a candidate, applies Enqueue/Cancel ops in order against the candidate (allocating revisions from a local counter), and checks the final capacity only at the end. On success the candidate, clock, generation (+1 exactly once) and next-revision are committed in one shot; on any failure (`ErrExists`, `ErrNotFound`, `ErrCapacity`, `ErrTime`) nothing is committed, so time, state and revisions roll back for free. Empty batches are validated but change nothing.

### Ownership

All mutable state sits behind one `sync.Mutex`, so every public method is linearizable. `Snapshot` and `Pop` return freshly allocated slices detached from internal state. `Clone` deep-copies the item map and the logical clocks (now, generation, nextRevision) into a fully independent queue — mutating either side never aliases the other.

### Complexity

- `New`, `ValidateBatch`: O(1) / O(batch).
- `Apply`: O(n + b) for n existing items and b ops (candidate copy + in-order ops), plus the final O(1) capacity check.
- `Pop`: O(n + k log k) for k ready items; deletion is O(k).
- `Snapshot` / `Clone`: O(n log n) / O(n). `Stats`: O(1).
