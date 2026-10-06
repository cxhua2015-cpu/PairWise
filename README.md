# readyqueue270

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

- `prioritybox.go` — core engine: `Queue`, `New`, `Apply`, `Pop`, `Snapshot`, ordering.
- `validation.go` — shared structural validation (`validateBatchStruct`) used by both `Apply` and the side-effect-free `ValidateBatch`, plus ID/name rules.
- `stats.go` — `Stats`, a linearizable summary read under the same mutex.
- `clone.go` — `Clone`, a deep copy preserving the logical clocks (`now`, `generation`, `nextRevision`).

## Design notes

### Index

Items live in a single `map[string]Item` keyed by ID, giving O(1) existence checks for `Enqueue`/`Cancel`. Ready items are selected and sorted on demand; no separate heap is maintained, so the map is the only source of truth and cannot drift out of sync with a secondary index.

### Candidate transaction

`Apply` first runs the shared structural validation (no state access), then checks the monotonic clock (`Batch.Now` must be >= current time, else `ErrTime`). It executes the ops in order against a *candidate* map copied from the live index, assigning revisions from a local counter. Capacity (`MaxItems`) is checked only once, at the end, against the final candidate size. Only on full success are the candidate map, clock, and revision counter committed; any failure (`ErrExists`, `ErrNotFound`, `ErrCapacity`) simply discards the candidate, so time, items, and revisions roll back for free. A non-empty successful batch increments `generation` exactly once; an empty batch leaves it unchanged.

### Ownership and concurrency

A single `sync.Mutex` guards all state, making every public method (`Apply`, `Pop`, `Snapshot`, `Stats`, `Clone`, `ValidateBatch`) safe for concurrent use and linearizable. `Snapshot` and `Pop` return freshly allocated slices, and `Clone` copies the map into a new queue, so callers never alias internal state and a clone is fully independent of its original.

### Complexity

- `Apply`: structural validation O(total op bytes); execution O(n + k) to clone the index and apply k ops, where n is the live item count.
- `Pop`: O(n + r log r) to scan for ready items and sort the r ready ones by Priority desc, ReadyAt asc, ID asc.
- `Snapshot` / `Clone`: O(n) time and space; `Snapshot` adds O(n log n) sorting.
- `Stats` / `ValidateBatch`: O(1) and O(batch size) respectively.
