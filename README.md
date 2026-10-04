# scoreboard

Concurrency-safe, in-memory transactional scoreboard for the control plane.
Go 1.22+, standard library only. The full contract lives in `SPEC.md`.

## Design

### Index

The board keeps a single `map[string]Entry` guarded by a `sync.RWMutex`
(`scoreboard/scoreboard.go`). There is no separate sorted index: `Range`,
`Snapshot` and `Changed` materialize and sort matching entries on demand with
`sort.Slice`, ordered by score descending then member ascending. This keeps
writes O(1) per op and avoids index maintenance on the commit path.

### Candidate transactions

`Apply` runs in two phases under one write lock:

1. **Structural validation** of every op in input order, without reading
   state (kind, member charset/length, Score/Delta shape per kind).
2. **Execution** in input order on an isolated candidate: a fresh copy of
   the member map plus a local `nextRev` counter. Only when all ops succeed
   and the *final* member count fits `MaxMembers` is the candidate swapped
   in and the counter committed. Any error (`ErrNotFound`, `ErrOverflow`,
   `ErrCapacity`) discards the candidate, so state, generation and revision
   allocation are rolled back as a whole.

Because capacity is checked only after all ops, a batch may delete before it
creates to stay within the limit.

### Revision and generation

`nextRev` starts at 1. Each Upsert/Increment allocates the current value and
bumps it; Delete allocates nothing. Failed batches never consume revisions.
`Result.Revision` is the latest revision allocated by that batch (0 when the
batch allocated none). A successful nonempty batch increments `generation`
exactly once; an empty batch changes nothing.

### Sorting and pagination

Entries order by `(Score desc, Member asc)`. A set cursor `(Score, Member)`
selects only entries strictly after it in that ordering — even if the cursor
member no longer exists — so keyset pagination is stable across concurrent
writes. `Range` validates `min <= max`, `limit` in 1..1000, and cursor member
syntax. `Get`, `Range` and `Snapshot` return freshly allocated slices, so
callers own their results.

### Complexity

Let `n` = members, `k` = ops in a batch, `m` = entries matching a range.

- `Apply`: O(n + k) to copy the candidate map, O(k) to execute, O(k log k)
  to sort `Changed`.
- `Get`: O(1) average.
- `Range`: O(n + m log m) — full scan, filter, sort, then truncate to `limit`.
- `Snapshot`: O(n log n).

All operations are serialized for writers (`Apply`) and concurrent for
readers (`Get`/`Range`/`Snapshot`) via the RW mutex.

## Tests

`scoreboard/contract_test.go` pins the public contract;
`scoreboard/scoreboard_test.go` adds coverage for repeated ops in a batch,
delete-then-recreate, revision rollback on failure, int64 boundary
arithmetic, final-capacity semantics, cursor pagination boundaries, tie-break
sorting, result ownership isolation, and mixed concurrent load.

Run:

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
