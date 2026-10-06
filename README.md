# metacatalog281

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

- `servicecatalog.go` — store state, atomic `Apply`, `Get`, `Snapshot`.
- `validation.go` — shared structural precheck (`ValidateBatch`), reused by `Apply`.
- `stats.go` — linearizable `Stats` summary.
- `clone.go` — clock-preserving, ownership-isolating `Clone`.

## Index

Records live in a `map[string]Record` keyed by name, giving O(1) average
lookup for `Get` and for the per-op work inside a batch. `Snapshot` (and the
`Changed` list in `Result`) sorts the keys, so ordered output costs
O(n log n). Two logical clocks accompany the map: `generation`, bumped once
per non-empty successful batch, and `nextRevision`, allocated only to Put
ops. A running `totalBytes` counter keeps end-of-batch capacity checks O(1).

## Candidate transaction

`Apply` never mutates committed state speculatively. It first runs the
shared structural precheck (no state reads), then replays the ops in input
order on a *candidate*: a shallow copy of the record map plus scratch clock
counters. Record-count and total-value-byte limits are checked only once, at
the end of the batch. On any failure (`ErrNotFound`, `ErrCapacity`) the
candidate is discarded, so state, generation and revision roll back for
free; on success the candidate map and clocks are swapped in under the
write lock, making the batch atomic and linearizable.

## Ownership

Committed record values are never mutated in place. Put copies the caller's
bytes on the way in; `Get`, `Snapshot`, `Result.Changed` and `Clone` copy
them on the way out. `Clone` therefore shares no mutable memory with the
original — batches applied to either store cannot leak into the other —
while still preserving the logical clocks (generation and nextRevision).

## Complexity

- `Get`, `Stats`: O(1) under a read lock.
- `Apply`: O(k · v) to copy k op values of size v, plus O(c log c) to sort
  the c changed names; one write-lock hold per batch.
- `Snapshot`, `Clone`: O(n · v) to deep-copy n records, plus O(n log n)
  sorting for `Snapshot`.

All public methods are safe for concurrent use; a single `sync.RWMutex`
linearizes every operation.
