# quorumlog

`quorumlog` tracks quorum commits for multiple replicated log streams. The
public contract is defined by `SPEC.md` and `quorumlog/contract_test.go`.
Go 1.22+, standard library only.

## Internal indexes

Each stream keeps:

- `entries`: `map[index] -> {digest, payload}` for uncommitted entries.
- `acks`: `map[index] -> map[replica] -> digest` for uncommitted ACK records.
- `committedDigests`: `map[index] -> digest` retained after commit for
  idempotency/conflict checks of late duplicates (payloads are dropped).
- Scalars: committed prefix, seal state (`sealed`, `lastIndex`),
  `highestObserved` index (for seal validation), `lastActivity`, `completed`
  flag, per-stream buffered byte count, and a pending-ACK counter.
- The voter set is canonicalized to a sorted slice plus a hash set for O(1)
  voter membership checks.

The tracker holds all streams in a `map[streamID] -> *stream` guarded by a
single `sync.Mutex`, plus a global buffered-byte counter.

## Continuous commit algorithm

After every successfully applied entry or ACK, a loop examines
`Committed+1`: it commits only if the entry is present and at least `Quorum`
distinct configured voters have ACKed a digest equal to the entry's digest
(mismatching ACKs are ignored, not blocking). The loop stops at the first
missing or under-acknowledged index. Each commit moves the payload into the
`Outcome` (ownership transfer, no extra copy), deletes the pending entry and
its ACK records, retains the digest, and decreases buffered bytes. Completion
is reported exactly once: when a sealed stream's committed prefix first
reaches `LastIndex` (including the empty-log seal at index 0).

## Batch transaction strategy

`ApplyBatch` deep-copies the stream map (immutable `pendingEntry` values are
shared; all maps are rebuilt) and applies updates strictly in input order to
this isolated working copy. The first error aborts the batch and the working
copy is discarded, so no state, activity time, capacity counter, committed
output, or completion marker leaks. On success the working copy is swapped
in atomically. `Apply` is `ApplyBatch` with one element.

## Capacity accounting

Only `len(Payload)` of uncommitted entries counts toward
`MaxBufferedBytes`; ACK metadata, configuration, and committed digests do
not. The budget is checked once, after the whole batch succeeds: a batch may
temporarily exceed the limit if later updates in the same batch commit
entries and release bytes. If the final total exceeds the limit, the batch
returns `ErrCapacity` and is rolled back entirely.

## Payload ownership

Entry payloads are deep-copied on arrival; mutating the caller's slice
afterwards has no effect. Committed payloads in outcomes are the uniquely
owned stored copies (the pending entry is deleted at commit), so mutating an
outcome payload cannot corrupt internal state or other results. Snapshots
copy voter slices; no returned value aliases internal state.

## Expiry policy

`ExpireBefore(cutoff)` atomically removes every stream with
`LastActivity < cutoff` (equality retained), returning removals sorted by
stream ID with their committed prefix and buffered bytes just before
removal. Expiry deletes the complete stream state, so the ID can be opened
again afterwards. Activity time is monotonic per stream; any update or
reopen moving it backwards fails with `ErrTime`.

## Complexity

Let `S` be the number of streams, `P` the number of pending entries, `A` the
number of pending ACK records, `C` the number of retained committed digests,
and `B` the batch size.

- `Open`: O(V log V) to canonicalize V voters.
- `Apply`: O(1) amortized per update plus O(k·Q) for committing k entries
  whose ACK sets total Q.
- `ApplyBatch`: O(S + P + A + C) to clone the working state, plus the
  per-update costs above; space O(S + P + A + C) for the working copy.
- `ExpireBefore`: O(S log S). `Snapshot`: O(S log S + V + P + A).
- Total space: O(S + P + A + C + buffered payload bytes).

All public methods take a single mutex, so calls are linearizable and safe
for concurrent use; no user code runs while the lock is held.
