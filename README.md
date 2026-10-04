# deadlinequeue

Concurrency-safe, in-memory, multi-queue deadline task table for background
executors. Go 1.22+, standard library only. See `SPEC.md` for the full contract.

## Data structures and indexing

- A single `sync.Mutex` guards all state; every public method
  (`ApplyBatch`, `PopDue`, `Window`, `Snapshot`) is safe for concurrent use.
- Tasks live in one `map[string]Task` keyed by globally unique ID, plus a
  running `payloadBytes` counter and a `generation` counter.
- There are no secondary indexes: ordered reads filter by queue and sort on
  demand, per the SPEC's allowed complexity envelope.

## Ordering

Canonical order is queue ascending, Due ascending, Priority descending
(full `int32` range), then ID ascending. `PopDue` and `Window` use the
within-queue portion of that order; `Snapshot` uses the full global order.
Ties are fully broken by ID, so all outputs are stable.

## Transactions

`ApplyBatch` runs in three phases:

1. **Structural validation** of every change (valid Add task, or Delete with
   only `ID` set, known kind) before any state lookup.
2. **Sequential execution** in input order on an isolated candidate map
   cloned from current state. Add of an existing ID fails with `ErrExists`;
   Delete of a missing ID fails with `ErrNotFound`. Because changes resolve
   sequentially, Delete followed by Add of the same ID in one batch is valid.
3. **Capacity check** of the final candidate only: task count against
   `MaxTasks` and total Payload bytes against `MaxPayloadBytes`. Transient
   overflows that resolve within the batch are accepted.

Any failure discards the candidate — the whole batch rolls back and the
generation is untouched. A successful nonempty batch commits the candidate
and increments `generation` exactly once; an empty batch is a no-op that
returns the current generation. `PopDue` increments generation once iff it
removed at least one task.

## Capacity

`New` requires positive `MaxTasks`, `MaxPayloadBytes`, and `MaxNameBytes`.
IDs and queue names are nonempty and at most `MaxNameBytes` bytes; a single
Payload is at most `MaxPayloadBytes` bytes; Due is a nonnegative `int64`;
Payload must be non-nil (empty is fine). `PopDue` takes `limit` in
`[0,1000]` (0 = unlimited); `Window` takes `limit` in `[1,1000]` and the
half-open interval `start <= Due < end` with `0 <= start <= end`.

## Ownership

All Payload slices crossing the API boundary are deep-copied: added Payloads
are cloned on commit, and `Window`/`Snapshot` results are cloned on the way
out. Mutating an input or output slice never affects stored state, and
returned slices never alias each other.

## Complexity

Let `n` be the number of stored tasks and `b` the total Payload bytes.

| Operation   | Time                          | Extra space |
|-------------|-------------------------------|-------------|
| `ApplyBatch`| O(n + k) map ops + O(added b) | O(n + b) candidate clone |
| `PopDue`    | O(n log n) sort + O(removed)  | O(n)        |
| `Window`    | O(n log n)                    | O(n + returned b) |
| `Snapshot`  | O(n log n) + O(b) copies      | O(n + b)    |

Overall space is O(task metadata + Payload bytes). `k` is the batch size.

## Testing

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
