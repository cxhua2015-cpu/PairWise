# balanceledger407

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## Design notes

### Index

Account state lives in a single `map[string]Account` guarded by one
`sync.RWMutex`. No secondary index is maintained: `Top` and `Snapshot`
materialize and sort a fresh slice per call, which keeps writes O(1) and
avoids index invalidation bugs. Ordering rules are value-desc/name-asc for
`Top` and name-asc for `Snapshot`.

### Candidate transactions

`Apply` is a candidate transaction: it first runs the shared structural
validation (`ValidateBatch`), then simulates the batch on a private copy of
the accounts map. Overflow is detected before every addition, the absolute
value limit is enforced per resulting balance, and the account capacity is
checked only once at batch end. Only on full success are the map, generation
(+1), and next-revision counters committed; any failure discards the
candidate, leaving prior state untouched (full rollback). Empty batches are
no-ops and do not advance the generation.

### Ownership

All returned slices (`Result.Changed`, `Top`, `Snapshot.Accounts`) are newly
allocated and never alias internal state. `Clone` copies the accounts map and
logical clocks (generation, next revision) into a ledger with its own mutex,
so subsequent writes to either ledger are fully isolated.

### Complexity

- `Apply`: O(k + n) for k ops over n accounts (map copy per batch).
- `ValidateBatch`: O(k), no state access.
- `Top`: O(n log n); `Snapshot`: O(n log n); `Stats`: O(1).
- `Clone`: O(n).
