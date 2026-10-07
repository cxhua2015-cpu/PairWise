# readyqueue420

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

## Design notes

### Indexing

Items live in a single `map[string]Item` keyed by ID, giving O(1) existence
checks for `Enqueue`/`Cancel` and O(1) deletes in `Pop`. There is no
incremental heap: queue sizes are bounded by `Options.MaxItems`, so `Pop` and
`Snapshot` collect candidates and sort them by (Priority desc, ReadyAt asc,
ID asc) on demand. This keeps the transactional path simple and exactly
matches the spec ordering without maintaining a secondary index.

### Candidate transactions

`Apply` first runs the shared structural precheck (`validateBatch`, also used
by `ValidateBatch`) without touching state, then takes the queue lock and
executes all ops against a private candidate map copied from the live map.
Revisions are allocated from a local counter. Capacity is checked only once,
at the end, against the candidate. On success the candidate map, clock,
generation (+1) and next-revision counter are committed in one shot; on any
failure the candidate is discarded, so time, items and revisions roll back
for free. Empty batches are a no-op and never bump the generation.

### Ownership

All mutable state sits behind one `sync.Mutex`; every public method is safe
for concurrent use. `Snapshot` and `Pop` return freshly allocated slices,
`Clone` copies the item map and logical clocks into a new queue with its own
mutex, and `Stats`/`Clone` observe a linearizable state under the same lock.
No returned value aliases internal memory, so callers may mutate results
freely.

### Complexity

- `New`, `Stats`: O(1)
- `ValidateBatch`: O(B) in batch size, no state access
- `Apply`: O(N + B) for N live items (candidate copy) and B ops
- `Pop`: O(N + R log R) for R ready candidates
- `Snapshot`: O(N log N); `Clone`: O(N)
