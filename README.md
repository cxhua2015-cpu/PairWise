# expirytable414

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

## Design notes

### Index

Entries live in a single `map[string]Entry` keyed by the validated key. The map
is the only index: Put/Touch/Delete are O(1) lookups, while `Apply` candidate
expiry, `Expire`, `Snapshot`, and `Clone` are O(n) scans over live entries.
Returned entry lists are sorted by key for deterministic output.

### Candidate transaction

`Apply` runs in three phases under one write lock:

1. **Preflight** — the shared structural validator (`validation.go`) checks the
   whole batch without touching state; then the monotonic clock is checked
   (`ErrTime` if `Now` moved backwards).
2. **Candidate** — a fresh map is built from live entries, entries with
   `ExpiresAt <= Now` are dropped, and Put/Touch/Delete execute in order,
   allocating one revision per Put/Touch.
3. **Commit** — the final capacity check runs against the candidate; on any
   error the candidate is discarded, so evictions, the clock, generation, and
   revisions all roll back together. On success the candidate is swapped in,
   `Now` advances, and generation increases exactly once (empty batches are a
   no-op).

### Ownership

All public methods are concurrency-safe via a single `sync.RWMutex`; `Stats`
and `Snapshot` are linearizable read-lock snapshots. `Snapshot` and `Expire`
return freshly allocated slices, and `Clone` copies the entry map plus the
logical clocks (`Now`, generation, next revision), so no returned value ever
aliases internal state and clones are fully independent.

### Complexity

- `New`, `ValidateBatch` per op, `Stats`: O(1) / O(ops)
- `Apply`: O(n + ops) time, O(n) candidate space
- `Expire`, `Snapshot`, `Clone`: O(n) (plus O(n log n) sort for returned slices)
