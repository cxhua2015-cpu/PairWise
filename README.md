# metacatalog246

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## Design notes

### Index
The store keeps a single in-memory hash index `map[string]Record` keyed by name,
guarded by one `sync.RWMutex`. All reads (`Get`, `Snapshot`, `Stats`, `Clone`)
take the read lock; `Apply` takes the write lock, so every public method is
concurrency-safe and linearizable.

### Candidate transactions
`Apply` first runs the shared side-effect-free structural validation
(`ValidateBatch` in `validation.go`), then applies the ops in input order to a
private candidate map. `Put` allocates the next consecutive revision; `Delete`
allocates none and fails with `ErrNotFound` for missing names. Record-count and
total-value-byte capacities are checked only at the end of the batch against the
candidate. On any failure the candidate is discarded — records, generation and
revision are untouched (full rollback). On success the candidate is swapped in
and generation advances exactly once per non-empty batch.

### Ownership
All `Value` byte slices are deep-copied on the way in (`Apply`) and on the way
out (`Get`, `Snapshot`, `Result.Changed`, `Clone`), so callers can never mutate
internal state and the store never retains caller buffers. `Clone` (in
`clone.go`) preserves the logical clocks (generation, next revision) while being
fully independent of the original. `Stats` (in `stats.go`) is a linearizable
summary computed under the read lock.

### Complexity
- `Apply`: O(R + B) time and O(R + B) space, where R = current records, B = batch ops.
- `Get`: O(1) average. `Stats`: O(R). `Snapshot` / `Clone`: O(R) plus O(R log R) sort for `Snapshot`.
- `ValidateBatch`: O(B), no state access.
