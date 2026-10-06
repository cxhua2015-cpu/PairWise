# expirytable259

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## Design notes

### Indexing

Entries live in a single `map[string]Entry` owned by `Table`, guarded by one
`sync.Mutex`. There is no secondary time index: expiry scans the map, which
keeps the invariant "every stored entry has `ExpiresAt > Now`" trivial to
maintain. `Snapshot` and `Expire` return entries sorted by key so output is
deterministic regardless of map iteration order.

### Candidate transactions

`Apply` never mutates live state until the batch fully succeeds. It first runs
the shared structural validation (`validateBatch`), then checks monotonic
time, then builds a candidate map: live entries with `ExpiresAt <= Now` are
dropped, and `Put`/`Touch`/`Delete` execute in order against the candidate,
allocating revisions from a local counter. Final capacity is enforced on the
candidate. Only on success are the map, clock, revision counter and
generation committed, so any error (`ErrTime`, `ErrNotFound`, `ErrCapacity`)
rolls back evictions, time and revisions atomically. A non-empty successful
batch increments `generation` exactly once; an empty batch changes nothing.

### Validation, stats, clone

- `validation.go`: `ValidateBatch` and `Apply` share `validateBatch`, a
  side-effect-free check of key charset/length, op kinds, non-negative time,
  and the rule that `Put`/`Touch` require `ExpiresAt > Now`.
- `stats.go`: `Stats` takes the same lock and reports a linearizable summary
  consistent with any concurrent transaction.
- `clone.go`: `Clone` deep-copies the entry map and all logical clocks under
  the lock; the clone owns its storage outright, so later mutations of either
  table never alias the other.

### Ownership

All returned slices (`Snapshot.Entries`, `Expire` results) are freshly
allocated; callers cannot reach internal state. `Clone` shares no memory with
its source.

### Complexity

Let `n` be the number of stored entries and `m` the batch size. `Apply` is
`O(n + m)` time and `O(n + m)` extra space for the candidate map. `Expire` is
`O(n + k log k)` for `k` expired entries (scan plus sort). `Snapshot` and
`Clone` are `O(n log n)` and `O(n)` respectively. `Stats` and
`ValidateBatch` are `O(1)` and `O(m)`.
