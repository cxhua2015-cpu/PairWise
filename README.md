# balanceledger272

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

## Design notes

### Indexing

Account state lives in a single `map[string]Account` guarded by a `sync.RWMutex`. There are no secondary indexes: `Top` and `Snapshot` materialize and sort a fresh copy of the map on each call, so every returned slice is fully isolated from internal state. Reads (`Top`, `Snapshot`, `Stats`, `Clone`, `ValidateBatch`) take the read lock; `Apply` takes the write lock.

### Candidate transactions

`Apply` first runs complete structural validation (shared with `ValidateBatch` via `validateOp`) without touching state. It then stages all mutations on a private copy of the account map — the candidate transaction — checking int64 overflow before arithmetic and enforcing `MaxAbsValue` per operation. Final account capacity is checked once at batch end. Only on success is the staged map swapped in and `generation` incremented exactly once; any failure (`ErrInvalidInput`, `ErrValue`, `ErrNotFound`, `ErrCapacity`) simply discards the candidate, giving full rollback with no undo log.

### Ownership

`Clone` copies the map, options, and logical clocks (`generation`, `revision`) under the read lock; the clone shares no memory with the original and can be mutated independently. `Result.Changed`, `Top`, and `Snapshot` all return freshly allocated slices, so callers can never observe or corrupt internal state.

### Complexity

- `Apply`: O(k·n) worst case to copy the map for a batch of k ops over n accounts (O(n) copy + O(k) application).
- `Top`: O(n log n) sort, then O(m) truncation for m results.
- `Snapshot`: O(n log n) sort by name.
- `Stats`: O(1). `ValidateBatch`: O(k). `Clone`: O(n).
