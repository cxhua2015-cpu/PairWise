# quorumlog

`quorumlog` tracks quorum commits for multiple replicated log streams. The
public contract is defined by `SPEC.md` and `quorumlog/contract_test.go`; the
implementation lives in `quorumlog/quorumlog.go` and uses only the Go standard
library (Go 1.22+).

## Internal indexes

Each stream keeps:

- `entries`: map `index -> {digest, payload}` for uncommitted entries.
- `acks`: map `index -> (replica -> digest)` for pending ACK records.
- `digests`: map `index -> digest` for already-committed indexes, retained
  for the stream's lifetime so late duplicates/conflicts on committed indexes
  can be classified without keeping committed payloads.
- `committed` (prefix length), `sealed`/`lastIndex`, `maxObserved` (highest
  index seen in any entry or ACK, used to reject too-low seals),
  `lastActivity`, and a per-stream `buffered` byte counter.

The tracker holds a `map[streamID]*stream` plus a global `buffered` counter
that always equals the sum of per-stream counters.

## Continuous-commit algorithm

After every accepted entry or ACK, the stream repeatedly examines
`committed+1`: it commits only if the entry is present and at least `Quorum`
distinct configured replicas have ACKed the entry's digest (mismatching ACKs
are recorded but ignored for quorum and never block matching ones). The loop
stops at the first missing or under-acknowledged index. Each committed index
removes its pending entry and ACK records, decreases buffered bytes, retains
its digest in `digests`, and appends a `CommittedEntry` with a fresh payload
copy to the outcome, in increasing index order. Completion is flagged exactly
once, on the update that first makes a sealed stream's committed prefix reach
`LastIndex` (an empty stream sealed at index 0 completes immediately).

## Batch transaction strategy

`ApplyBatch` clones each touched stream on first use inside the batch (deep
copy of the mutable maps; immutable voter data is shared) and applies updates
strictly in input order against the clones, tracking the global buffered
delta per update. Any validation, conflict, time, or seal error aborts the
batch and discards the clones, so no state, activity time, capacity counter,
committed output, or completion marker becomes visible; the first error in
input order wins. Only after all updates succeed is the final buffered total
checked against `MaxBufferedBytes` — temporary overflow inside a batch is
allowed if later commits release bytes. On success the clones replace the
originals atomically under the tracker mutex. `Apply` is `ApplyBatch` of one
element.

## Capacity accounting

Only `len(Payload)` of uncommitted entries counts toward
`MaxBufferedBytes`. ACK metadata, configuration, committed digests, and other
bookkeeping are excluded. Committing an entry subtracts its payload length;
expiry subtracts the removed stream's remaining bytes.

## Payload ownership

Entry payloads are deep-copied on ingestion, so callers may reuse or mutate
their buffers. Stored payloads are never mutated in place, which lets batch
clones share the byte slices safely. Payloads returned in `Outcome.Committed`
are fresh copies, so mutating an outcome cannot corrupt internal state or
other results. `Snapshot` returns freshly copied voter slices and scalar
counts; nothing returned aliases internal state.

## Expiry policy

`ExpireBefore(cutoff)` atomically removes every stream with
`LastActivity < cutoff` (equality retained), returning one `ExpiredStream`
per removal sorted by stream ID with the committed prefix and buffered bytes
immediately before removal. Expiry drops the complete stream state, so an
expired ID may be reopened with any configuration. Activity time is
monotonic per stream: any open or update with `At < LastActivity` fails with
`ErrTime` and leaves state unchanged.

## Concurrency

A single `sync.Mutex` guards the whole tracker, making all public methods
linearizable. No user code runs while the lock is held.

## Complexity

Let `n` be the number of streams, `k` the batch size, `p` the number of
pending entries/ACKs of a stream, and `c` the number of entries committed by
one update.

- `Open`: `O(v log v)` for `v` voters (canonical sort).
- `Apply` / per-update work in `ApplyBatch`: `O(1)` map operations plus
  `O(c · (a + L))` for commits, where `a` is ACKs at the committing index
  and `L` the payload bytes copied for outcomes. A batch additionally pays a
  one-time `O(p)` clone per touched stream.
- `ExpireBefore`: `O(n)` scan plus `O(r log r)` to sort the `r` removals.
- `Snapshot`: `O(n log n)` sort plus `O(total pending)` counting.
- Space: `O(pending entries + pending ACKs + committed indexes)` per stream;
  committed payloads are not retained, only their digests.
