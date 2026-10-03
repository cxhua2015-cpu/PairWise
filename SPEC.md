# quorumlog specification

Implement package `quorumlog` using Go 1.22+ and only the standard library.
The declarations in `quorumlog/quorumlog.go` are the public API and must not be
removed or have their signatures changed.

## Limits and terminology

- A stream, voter, replica, and digest is a non-empty string of at most 128
  bytes.
- Logical activity time `At` and expiry cutoffs are integers in
  `[0, 1_000_000_000_000]`.
- Log indexes are in `[1, 1_000_000_000_000]`. A seal's `LastIndex` may also be
  zero, representing an empty log.
- `Options.MaxBufferedBytes` must be positive.
- A stream configuration has a non-empty, duplicate-free voter set and a
  quorum in `[1, len(Voters)]`. Voter order is not significant.
- "Buffered bytes" means the sum of `len(Payload)` for uncommitted entries.
  ACK metadata, configuration, committed digests, and bookkeeping do not count
  toward this configured budget.

Invalid constructor arguments, malformed identifiers, invalid bounds, an
unknown ACK replica, or an update with anything other than exactly one payload
return `ErrInvalid`, except that an otherwise valid update for a stream that
has not been opened returns `ErrUnknownStream`.

## Opening streams

`Open(config, at)` creates a stream with committed prefix zero. Voter order is
canonicalized lexicographically. Reopening an existing stream with the same
voter set and quorum is idempotent and advances its activity time. Reopening it
with a different configuration returns `ErrConflict`. Activity time may never
move backwards; otherwise `ErrTime` is returned. Errors leave state unchanged.

An expired stream ID may be opened again because expiry removes the complete
stream state.

## Updates and activity time

`Update.Stream` selects an open stream and `Update.At` is its new activity
time. An update contains exactly one of `Entry`, `Ack`, or `Seal`. Successful
updates, including exact duplicates, set the stream's activity time to `At`.
Moving a stream's activity time backwards returns `ErrTime` atomically.

`Apply(u)` has exactly the same semantics as `ApplyBatch([]Update{u})` and
returns that element's outcome.

### Entries

Entries may arrive in any index order. The implementation owns a deep copy of
`Payload`. Repeating the same index, digest, and bytes is idempotent. Reusing an
index with a different digest or bytes returns `ErrConflict`.

For an already committed index, the retained committed digest makes an entry
with that digest an idempotent no-op; a different digest returns `ErrConflict`.
The payload is not compared after commit because committed payloads are not
retained.

### ACKs

An ACK may arrive before its entry. It is identified by `(Index, Replica)`.
Repeating that pair with the same digest is idempotent; changing its digest
returns `ErrConflict`. Only ACKs from configured voters are valid.

ACKs from different replicas may disagree. Only distinct configured replicas
whose ACK digest equals the entry digest count toward quorum. A mismatching ACK
does not block other matching ACKs from committing the entry.

For an already committed index, an ACK whose digest equals the retained
committed digest is an idempotent no-op; a different digest returns
`ErrConflict`.

### Seals

A seal declares the final index and may arrive before entries or ACKs. An exact
duplicate is idempotent. A different second seal returns `ErrConflict`. A seal
below the highest index already observed or below the committed prefix returns
`ErrConflict`. After sealing, an entry or ACK above `LastIndex` returns
`ErrSealed`.

An empty stream completes when sealed at index zero. Otherwise it completes
when its committed prefix reaches `LastIndex`. `Outcome.Completed` is true only
on the successful update that first causes completion; later idempotent updates
return false.

## Continuous quorum commit

After applying each entry or ACK, repeatedly examine `Committed+1`. It commits
only when that entry is present and at least `Quorum` distinct voters have ACKed
the entry's digest. Stop at the first missing or under-acknowledged index even
if later indexes have quorum.

Every newly committed entry is returned in `Outcome.Committed` in increasing
index order. Payloads in outcomes are fresh copies that cannot mutate internal
state or another result. Committing removes the pending entry payload and all
ACK metadata at that index and decreases buffered bytes. The committed digest
is retained for later idempotency and conflict checks.

## Batches and capacity

`ApplyBatch` processes updates strictly in input order against an isolated
transactional state and returns one `Outcome` per input in the same order.

- If any update fails, return `nil` and that update's error. No stream state,
  activity time, capacity counter, committed output, or completion marker from
  the batch becomes visible.
- The first error encountered in input order wins; implementations must not
  prevalidate later elements in a way that changes error precedence.
- Buffered bytes may temporarily exceed `MaxBufferedBytes` inside a batch if
  later updates in that same batch commit entries and release bytes. Check the
  capacity only after every update succeeds. If the final total exceeds the
  limit, return `ErrCapacity` and roll back the entire batch.
- An empty batch succeeds with a non-nil empty result and no state change.

## Expiry and snapshots

`ExpireBefore(cutoff)` atomically removes streams whose `LastActivity < cutoff`.
Equality is retained. It returns one `ExpiredStream` per removal sorted by
stream ID, including the committed prefix and buffered byte count immediately
before removal. An invalid cutoff returns `ErrInvalid` without mutation.

`Snapshot` returns streams sorted by ID. Each stream's `Voters` is sorted,
copied, and owned by the caller. Counts describe currently pending entries and
ACK records. `LastIndex` is zero when unsealed. Returned values and slices must
not alias internal state.

## Concurrency and documentation

All public methods must be safe for concurrent calls and linearizable. Do not
invoke user code while holding internal state (the public API provides no need
to do so).

Update `README.md` with the internal indexes, continuous-commit algorithm,
transaction strategy, exact capacity accounting, payload ownership, expiry
policy, and actual time/space complexity.
