# readyqueue425

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

## Design notes

### Index

The queue keeps its items in a single `map[string]Item` keyed by ID, guarded by one `sync.Mutex`. The map gives O(1) existence checks for `Enqueue`/`Cancel` inside a batch. Canonical pop order (Priority desc, ReadyAt asc, ID asc) is produced on demand by sorting, so there is no secondary heap to keep consistent during transactional rollback.

### Candidate transactions

`Apply` first runs the shared structural validation (`ValidateBatch`), then executes the whole batch against a private copy of the item map and a local revision counter. Time, items, generation and revision are committed only after the final capacity check succeeds, so any failure (`ErrTime`, `ErrExists`, `ErrNotFound`, `ErrCapacity`) leaves the queue bit-for-bit untouched. `Preview` reuses the exact same semantics: it clones the queue on one linearizable snapshot and runs `Apply` on the clone, returning the candidate `Result`, `Snapshot` and `Stats` while the receiver's state, generation, revision and logical clock stay unchanged; failures return the same error as `Apply` with all other values zeroed.

### Ownership

All returned slices (`Pop`, `Snapshot`, `Preview`) are freshly allocated and sorted copies; callers may mutate them freely without affecting the queue. `Clone` deep-copies the item map and preserves the logical clocks (`now`, `generation`, `nextRevision`), so original and clone evolve independently. `Options` is immutable after `New`, which lets `ValidateBatch` run without taking the lock or reading mutable state.

### Complexity

- `New`, `Stats`: O(1).
- `ValidateBatch`: O(total ID bytes), no state access.
- `Apply`: O(n + k) for n stored items and k ops (one map copy plus O(1) per op).
- `Pop`: O(n log n) sort over ready items, O(1) deletion per popped item.
- `Snapshot`, `Clone`: O(n) and O(n log n) respectively; `Preview` is `Clone` + `Apply` + `Snapshot` + `Stats`.
