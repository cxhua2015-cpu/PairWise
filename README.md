# expirytable429

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

## Design notes

### Index

Entries live in a single `map[string]Entry` keyed by the validated key. The map gives O(1) average lookup for Put/Touch/Delete; there is no secondary time index, so eviction scans the map. `Snapshot` and `Expire` return entries sorted by key for deterministic output, and every returned slice is freshly allocated and isolated from internal state.

### Candidate transaction

`Apply` first runs the shared structural validation (`validation.go`), then checks monotonic time (`Now >= now`). It copies the entry map into a candidate, evicts entries with `ExpiresAt <= Now` (closed boundary, same as `Expire`), and executes ops in order, assigning a revision to each Put/Touch. The final capacity check runs after all ops; on `ErrCapacity`, `ErrNotFound`, or any other failure the candidate, logical clock, and revision counter are discarded together — the committed state is untouched. A successful non-empty batch increments `generation` exactly once; an empty batch leaves it unchanged.

### Ownership

A single `sync.RWMutex` guards all state, so every public method is concurrency-safe and linearizable. `Clone` deep-copies the map and logical clocks under a read lock; the clone shares no memory with the original. `Preview` copies the state under a read lock, then runs the exact same transaction code on the private copy, returning the candidate `Result`, `Snapshot`, and `Stats` while the receiver's state, generation, revisions, and clock stay unchanged; failures return zero values with the same error and priority as `Apply`.

### Complexity

- `Apply` / `Preview`: O(n + m) for n entries and m ops (candidate copy + eviction scan + ops).
- `Expire`: O(n + k log k) for k expired entries (scan + sort).
- `Snapshot` / `Clone`: O(n log n) / O(n).
- `Stats`: O(1). `ValidateBatch`: O(m) with no state access.
