# metacatalog221

Concurrency-safe in-memory metadata catalog for a distributed control plane.
Go 1.22+, standard library only. Semantics are defined by `SPEC.md` and the
contract tests in `metacatalog221/contract_test.go`.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine,
side-effect-free validation, linearizable statistics, and ownership-safe
cloning. All four components are required by the public contract.

- `servicecatalog.go` — core store, atomic `Apply`, `Get`, `Snapshot`.
- `validation.go` — shared structural preflight (`validateBatch`) used by both
  `Apply` and the public `ValidateBatch`; never reads or mutates state.
- `stats.go` — `Stats`, a linearizable summary read under the same lock as
  transactions, so it always reflects a consistent committed state.
- `clone.go` — `Clone`, a fully independent deep copy that preserves the
  logical clocks (`generation`, `nextRevision`).

## Design notes

**Index.** Records live in a `map[string]*Record` keyed by name, guarded by a
single `sync.RWMutex`. Writers (`Apply`) take the write lock; readers (`Get`,
`Snapshot`, `Stats`, `Clone`) take the read lock and may proceed concurrently.
A running `totalValueBytes` counter is maintained transactionally so capacity
checks are O(1).

**Candidate transaction.** `Apply` first runs the shared structural preflight
(unknown kinds, illegal names, oversized values, non-nil Delete payloads are
rejected before any state is read). It then clones the record index header
(shallow copy of the map, values copied on write) into a candidate, replays the
ops in input order — each `Put` allocates the next consecutive revision,
`Delete` allocates none and fails with `ErrNotFound` for missing names — and
only at the end checks `MaxRecords` and `MaxTotalValueBytes` against the final
candidate state. On any failure the candidate is discarded, so state,
generation and revision roll back for free; on success the candidate is
swapped in and a non-empty batch bumps `generation` exactly once.

**Ownership.** Every byte slice crossing the API boundary is copied: `Put`
values are copied in, and `Get`/`Snapshot`/`Result.Changed`/`Clone` copy
values out. Callers can never mutate internal state, and the store never
aliases caller memory. `Clone` shares nothing with the original.

**Complexity.** For a batch of `k` ops over `n` records: `Apply` is O(n + k)
time and O(n) extra space for the candidate index; `Get` is O(1) plus an
O(v) value copy; `Snapshot` is O(n log n) for name sorting; `Stats` is O(1);
`Clone` is O(n + total value bytes).

## Verification

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
