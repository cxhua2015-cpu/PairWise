# balanceledger432

Concurrency-safe in-memory balance ledger (Go 1.22+, standard library only).
Atomic batches execute Add/Set/Delete in input order; Add/Set allocate
consecutive revisions. Overflow is detected before arithmetic, absolute-value
limits are enforced per operation, and the account capacity is checked only at
the end of a batch. Failures roll back the whole batch. `Top` orders by value
descending then name ascending; `Snapshot` orders by name.

## Multi-file architecture

- `creditpool.go` — core transaction engine: public types, `Ledger` state,
  `Apply`, `Top`, `Snapshot`.
- `validation.go` — side-effect-free structural precheck shared by
  `ValidateBatch` and `Apply` (kind, name charset/length, delta/value bounds).
- `stats.go` — linearizable `Stats` summary under the read lock.
- `clone.go` — deep copy preserving logical clocks (generation, nextRevision).
- `preview.go` — candidate transaction: replays full `Apply` semantics on a
  clone taken from one linearizable snapshot, returning candidate `Result`,
  `Snapshot` and `Stats` without touching the receiver's state or clocks.

## Indexing and concurrency

Accounts live in a `map[string]Account` guarded by a single `sync.RWMutex`.
Writers (`Apply`) take the exclusive lock; readers (`Top`, `Snapshot`,
`Stats`, `Clone`, `Preview`) take the read lock, so all public methods are
safe for concurrent use and every read observes a consistent state.

## Candidate transactions

`Preview` clones the ledger under the read lock (one linearization point),
applies the batch to the clone, and reports the candidate `Result`, `Snapshot`
and `Stats`. Errors and their priority match `Apply` on the same state; on
failure all return values are zero. The receiver's state, generation, revision
and logical clocks are never modified.

## Ownership

All returned slices (`Result.Changed`, `Top`, `Snapshot.Accounts`) are freshly
allocated and detached from internal state. `Clone` copies the account map, so
the clone and the original share no mutable memory; mutations on one never
leak into the other.

## Complexity

- `Apply`: O(k + n) for k ops and n accounts (state copy for rollback), plus
  O(k) revision allocation.
- `ValidateBatch`: O(k), no state access.
- `Top`: O(n log n) sort, then O(1) truncation to n entries.
- `Snapshot`: O(n log n) sort by name.
- `Stats`: O(1).
- `Clone`: O(n). `Preview`: O(n + k) plus the cost of `Apply` on the clone.
