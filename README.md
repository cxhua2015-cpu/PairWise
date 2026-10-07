# readyqueue410

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

## Design notes

### Index

Items live in a single `map[string]Item` keyed by ID, guarded by one `sync.Mutex`, so every public method is linearizable. Pop/Snapshot sort the ready candidates on demand by (Priority desc, ReadyAt asc, ID asc); with a small control-plane queue this avoids maintaining a secondary heap and keeps cancel/rollback trivial.

### Candidate transaction

`Apply` first runs the shared structural validation (`validateBatch` in `validation.go`, also used by `ValidateBatch`), then enforces monotonic time, then replays the ops against a private copy of the map. Duplicate IDs, missing cancels and the final capacity check (evaluated only at the end) abort before commit, so time, state and revision counters roll back automatically. On success the candidate map, clock, generation and next revision are committed atomically; an empty batch changes nothing.

### Ownership

`Snapshot` and `Pop` return freshly allocated slices detached from internal state, and `Clone` deep-copies the item map together with the logical clocks (now, generation, next revision), so the clone is fully independent of the original.

### Complexity

- `Apply`: O(n + k) for n items and k ops (map copy plus op replay).
- `Pop`: O(n + r log r) for r ready items (scan plus sort).
- `Snapshot`/`Clone`: O(n log n) / O(n).
- `Stats`/`ValidateBatch`: O(1) / O(k), with no state access for validation.
