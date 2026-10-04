# expiringstore

A concurrency-safe, in-memory key/value store with explicit-time expiration,
implemented in Go 1.22+ using only the standard library. See `SPEC.md` for the
normative contract.

## Data structures and indexing

- A single `map[string]entry` is the only index; lookups, inserts, and deletes
  are O(1) average. Each entry stores the value bytes, its allocation
  `revision`, and its absolute `ExpiresAt` time.
- A single `sync.Mutex` guards all state (`now`, `generation`, `nextRev`,
  `totalBytes`, and the map), so every public method is atomic and safe for
  concurrent callers.
- `Snapshot` and expired-key results are produced by collecting keys and
  sorting them with `sort.Strings`, giving deterministic, stable output.

## Expiration algorithm

Expiration is lazy and exact: an entry is expired precisely when
`ExpiresAt <= now`. There is no background goroutine or heap; every public
operation that carries a time (`Apply`, `Get`, `Sweep`) first scans the live
map and removes all entries due at that time, in O(n) over live entries.
This keeps the design simple and makes expiration atomic with the operation
that triggers it. `Get`/`Sweep` bump `generation` once only when at least one
entry actually expires.

## Batch transactions

`Apply` runs in three phases:

1. **Structural validation** of every op in input order (key charset/length,
   kind-specific field rules, `ExpiresAt > Batch.Now`) without touching state.
2. **Monotonic-time check**: `Batch.Now >= Snapshot.Now`, else `ErrTime`.
3. **Isolated candidate execution**: the live map is shallow-copied, due
   entries are expired into the candidate, then ops run sequentially in input
   order. `Put` creates/replaces and consumes the next revision (starting at
   1); `Delete`/`Touch` of a missing key fail with `ErrNotFound`; `Touch`
   changes only expiry. Final entry-count and total-byte capacity are checked
   after all ops.

Any failure (`ErrNotFound`, `ErrCapacity`) discards the candidate entirely:
the expiration sweep, time advance, generation bump, and revision allocation
all roll back with zero leakage. On success the candidate is committed,
`Now = Batch.Now`, and `generation` increments once if anything expired or
the batch had at least one op (a successful empty time-advance batch does not
increment it). `Result.Expired` is sorted by key; `Result.Revision` is the
latest allocated revision (0 before any `Put`).

## Time

Time is an explicit, nonnegative `int64` supplied by the caller; the store
starts at time zero and never reads the wall clock. Every time-carrying
operation requires `now >= Snapshot.Now` and returns `ErrTime` otherwise,
which keeps expiration semantics total and deterministic.

## Capacity

Limits come from `Options`: `MaxEntries` (live key count), `MaxTotalBytes`
(sum of live value bytes), `MaxValueBytes` (per-value), and `MaxKeyBytes`
(per-key). All must be positive and `MaxValueBytes <= MaxTotalBytes`, else
`New` returns `ErrInvalidOptions`. Entry-count and total-byte limits are
enforced on the final candidate state of a batch; per-key/per-value limits
are structural validation. A running `totalBytes` counter is maintained
incrementally so capacity checks are O(1).

## Ownership

All `Value` slices are deep-copied on the way in (`Apply`) and on the way out
(`Get`, `Snapshot`), so callers may freely reuse or mutate their buffers and
results without aliasing the store's internal state.

## Complexity

- `Get`: O(n) for the expiration sweep + O(1) lookup + O(v) copy of the value.
- `Sweep`: O(n + e log e) for n live entries and e expired keys (sorting).
- `Apply`: O(n) candidate copy and sweep + O(m·v) for m ops with value bytes
  + O(e log e) to sort expired keys.
- `Snapshot`: O(n log n) to sort keys + O(total bytes) to deep-copy values.

## Development

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
