# readyqueue415

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

## Design notes

### Index

Items live in a `map[string]Item` keyed by ID, giving O(1) existence checks for
`Enqueue`/`Cancel` and O(1) deletes during `Pop`. The canonical pop order
(priority desc, ReadyAt asc, ID asc) is computed on read by sorting, which keeps
writes cheap and the invariant trivial to reason about under a single mutex.

### Candidate transactions

`Apply` first runs the shared structural preflight (`validateBatch`, also used
by `ValidateBatch`) without touching state. It then clones the item map into a
candidate buffer and replays the ops in order against that buffer, assigning
revisions from a local counter. Capacity is checked only once, at the end,
against the candidate. The candidate, clock and revision counter are committed
only when every op succeeds, so any failure (`ErrTime`, `ErrExists`,
`ErrNotFound`, `ErrCapacity`) leaves time, state and revisions untouched —
rollback is structural, not compensating.

### Ownership

All mutable state sits behind one `sync.Mutex`; every public method is safe for
concurrent use. `Snapshot`, `Pop` and `Clone` return freshly allocated slices,
items and maps, so callers can never alias or mutate internal state. `Clone`
preserves the logical clocks (`now`, `generation`, `nextRevision`) while being
fully independent of the original.

### Complexity

- `Apply`: O(k·n) worst case for a batch of k ops over n items (candidate map copy O(n), each op O(1)); final capacity check O(1).
- `Pop`: O(n + r log r) for r ready candidates (scan + sort + O(1) map deletes).
- `Snapshot`/`Clone`: O(n log n) / O(n).
- `Stats`/`ValidateBatch`: O(1) / O(k·L) for ID length L.
