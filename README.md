# balanceledger222

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

- `creditpool.go` — core engine: `Ledger` state, `New`, `Apply`, `Top`, `Snapshot`, plus shared helpers (`checkedAdd`, `validName`, `withinAbsLimit`).
- `validation.go` — `ValidateBatch`: pure structural pre-check (known kinds, no stray fields, name charset/length). `Apply` calls the same function, so pre-check and transaction share one structural semantics.
- `stats.go` — `Stats`: linearizable summary read under the same lock as transactions.
- `clone.go` — `Clone`: deep copy preserving logical clocks with fully independent ownership.

## Design notes

### Index

Accounts live in a single `map[string]Account` keyed by name, giving O(1) average lookup/insert/delete per op. `Top` and `Snapshot` materialize and sort on demand; no secondary index is maintained, which keeps the write path O(1) and avoids index-consistency hazards during rollback.

### Candidate transaction

`Apply` first runs `ValidateBatch` (no state access), then takes the write lock and replays the ops in input order against a private copy of the account map plus a candidate `nextRevision`. Add/Set allocate consecutive revisions; Delete allocates none. int64 overflow is detected *before* arithmetic via `checkedAdd`, and the absolute-value limit is enforced per op; the account-capacity limit is checked only once, at batch end. Any failure (`ErrValue`, `ErrNotFound`, `ErrCapacity`) simply discards the candidate — the committed state is untouched, giving full rollback. On success the candidate map and clocks are swapped in and `generation` advances exactly once; empty batches change nothing.

### Ownership

All public methods are safe for concurrent use, guarded by a single `sync.RWMutex` (writers serialize, readers run concurrently). Returned slices (`Result.Changed`, `Top`, `Snapshot.Accounts`) are freshly allocated copies, so callers can mutate them freely. `Clone` copies the map under the read lock; the two ledgers share no memory afterwards, and mutations of one never alias the other.

### Complexity

- `Apply`: O(k + n) time, O(n) space for a batch of k ops over n accounts (map copy + per-op O(1)).
- `ValidateBatch`: O(k), no state access.
- `Top`: O(n log n) sort, O(n) space. `Snapshot`: O(n log n), O(n) space.
- `Stats`: O(1). `Clone`: O(n) time and space.
