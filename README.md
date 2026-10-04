# reorder

Concurrent-safe, in-memory multi-stream sequence reorderer for streaming
ingestion. See `SPEC.md` for the full contract. Standard library only;
requires Go 1.22+.

## Design

### Indexing

Each stream keeps `Next` (the next releasable sequence, starting at 1) and a
hash map `map[uint64][]byte` of buffered future events keyed by sequence.
The buffer holds all streams in a `map[string]*stream` guarded by a single
`sync.Mutex`, so every public method (`PushBatch`, `Skip`, `Delete`,
`Snapshot`) is safe for concurrent use.

### Contiguous release

An event with `Sequence == Next` is released immediately, `Next` is
incremented, and the per-stream map is repeatedly probed for `Next`,
releasing any consecutive buffered suffix in ascending sequence order.
Events with `Sequence > Next` are buffered as deep copies; events with
`Sequence < Next` fail with `ErrOldSequence`. A buffered sequence re-pushed
with a byte-identical payload is an idempotent no-op; a different payload
fails with `ErrConflict`.

### Transactions

`PushBatch` is atomic:

1. **Structural validation** of every input event (nonempty stream name
   within `MaxStreamBytes`, sequence in `[1, math.MaxUint64-1]`, non-nil
   payload) before any state is inspected.
2. **Isolated candidate application** in input order. Per-stream state is
   cloned lazily (copy-on-write); duplicate/conflict/old checks run against
   the candidate, so a mid-batch failure leaves the committed state
   untouched and returns no ready events.
3. **Capacity check** on the final candidate only: stream count
   (`MaxStreams`), buffered event count (`MaxBuffered`), and total buffered
   payload bytes (`MaxPayloadBytes`). Violations return `ErrCapacity` and
   roll back the whole batch.

A successful batch increments `Generation` exactly once iff the state
changed (stream creation, buffering, or `Next` advancement); idempotent
no-op batches and empty batches leave it unchanged. `Skip` and `Delete`
follow the same rule: a changing call bumps `Generation` once, no-ops
(`through < Next`) do not.

### Capacity accounting

Only retained future events count: `Buffered` is the number of buffered
events across all streams and `PayloadBytes` is the sum of their payload
lengths. Released events stop counting immediately.

### Ownership

All payloads are deep-copied at every boundary: accepted inputs are cloned
before storage, returned ready events are cloned from storage, and
`Snapshot` payloads are cloned again. No mutable backing storage is ever
shared between caller and buffer.

## Complexity

- `PushBatch`: average O(batch + released) map operations, plus O(batch)
  cloning for isolation; the final capacity tally is O(buffered).
- `Skip`: O(buffered in stream) to discard, plus the released suffix.
- `Delete`: O(1).
- `Snapshot`: O(s log s + n log n) to sort s streams and n buffered events.
- Space: O(streams + buffered payload bytes).

## Verification

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
