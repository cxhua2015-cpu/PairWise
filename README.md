# readyqueue245

A concurrency-safe, in-memory, ready-first priority queue for the distributed
control plane. Explicit non-negative monotonic time; `Apply` executes
Enqueue/Cancel batches atomically; `Pop` returns ready items ordered by
priority descending, ReadyAt ascending, ID ascending. Standard library only,
Go 1.22+.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine,
side-effect-free validation, linearizable statistics, and ownership-safe
cloning. All four components are required by the public contract.

- `prioritybox.go` — core engine: `Queue`, `New`, `Apply`, `Pop`, `Snapshot`,
  canonical ordering.
- `validation.go` — `ValidateBatch`: pure structural pre-check (time sign,
  op kinds, ID charset/length, Cancel field zeroing, Enqueue ReadyAt sign).
  It never reads or mutates queue state; `Apply` calls it so both share the
  exact same structural semantics.
- `stats.go` — `Stats`: linearizable summary (`Generation`, `NextRevision`,
  `Now`, `Items`) taken under the same mutex as mutations.
- `clone.go` — `Clone`: deep copy preserving the logical clocks (`now`,
  `generation`, `nextRevision`) with fully independent ownership.

## Indexing

Items live in a single `map[string]Item` keyed by ID, giving O(1) existence
checks for `Enqueue`/`Cancel` and O(1) deletes in `Pop`. There is no
persistent heap: `Pop` and `Snapshot` collect candidates and sort by the
canonical order (priority desc, ReadyAt asc, ID asc), which keeps the
transaction path simple and rollback trivial.

## Candidate transactions

`Apply` is a candidate transaction: the batch is first structurally
validated without touching state, then executed under one mutex hold against
a scratch of undo records. Each Enqueue assigns the next revision and records
an undo; each Cancel records the removed item. Capacity is checked only once,
at the very end. Any failure (`ErrExists`, `ErrNotFound`, `ErrCapacity`)
replays the undos in reverse and restores `nextRevision`, so time, item set
and revision counter are rolled back exactly. A non-empty successful batch
bumps `generation` exactly once and advances `now`; an empty batch is a
no-op that changes nothing.

## Ownership and concurrency

All public methods take the queue's single mutex, so every method is safe for
concurrent use and every observation (`Stats`, `Snapshot`, `Clone`) is
linearizable. Returned slices and maps are freshly allocated copies — callers
can never alias internal state, and a `Clone` shares no mutable memory with
its source (clocks included, then evolved independently).

## Complexity

- `New`, `ValidateBatch`: O(1) / O(batch size).
- `Apply`: O(k) for k ops, plus O(n) undo only on failure (n = items touched).
- `Pop`: O(m + r log r) with m items scanned and r ready candidates sorted.
- `Snapshot`, `Clone`: O(m log m) / O(m).
- `Stats`: O(1).

## Verification

`go test ./...`, `go test -race ./...`, `go run ./cmd/demo`.
