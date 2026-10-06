# expirytable249

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

## Design notes

### Index

Entries live in a single `map[string]Entry` keyed by the validated key string, giving O(1) average lookup for Put/Touch/Delete. A `sync.Mutex` guards the whole table, so every public method (`Apply`, `Expire`, `Snapshot`, `Stats`, `Clone`, `ValidateBatch`) is safe for concurrent use. `Snapshot` and `Expire` return entries sorted by key for deterministic output, and every returned slice is freshly allocated, so callers can never alias internal state.

### Candidate transaction

`Apply` runs in three phases:

1. **Structural validation** — shared with `ValidateBatch` (`validation.go`), pure and state-free: non-negative `Now`, known op kinds, key charset/length limits, `ExpiresAt > Now` for Put/Touch, and no extra fields on Delete.
2. **Time check** — `Batch.Now` must not move the monotonic clock backwards (`ErrTime`).
3. **Candidate execution** — the live map is copied into a candidate map, entries with `ExpiresAt <= Now` (closed interval) are evicted, and ops replay in order. Put/Touch allocate revisions from the table's logical counter. Not-found keys (`ErrNotFound`) and final capacity overflow (`ErrCapacity`) simply abandon the candidate, so evictions, the clock, the revision counter, and the generation all roll back together. Only a fully successful batch commits and bumps `generation` exactly once; empty batches change nothing.

`Expire(now)` applies the same closed-interval boundary (`ExpiresAt <= now`) directly against the live map and advances the shared monotonic clock.

### Ownership

`Clone` copies the entry map and all logical clocks (`now`, `generation`, `nextRevision`) under the lock, producing a fully independent table: subsequent mutations on either side never leak into the other. `Stats` and `Snapshot` are linearizable — they are taken under the same mutex that serializes transactions, so they always reflect a consistent committed state.

### Complexity

- `Apply`: O(n + m) for n live entries (candidate copy) and m ops, plus O(n) for the eviction scan.
- `Expire`: O(n + k log k) for k expired entries (scan plus deterministic sort).
- `Snapshot`: O(n log n) due to key-sorted output; `Stats`: O(1); `Clone`: O(n).
- `ValidateBatch`: O(m) and side-effect free.
