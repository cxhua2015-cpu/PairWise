# expiringstore

A concurrency-safe, in-memory expiring key-value store for Go 1.22+ with
explicit (caller-supplied) time. Standard library only. See `SPEC.md` for the
full contract.

## Design

**Index.** Entries live in a single `map[string]entry` guarded by one
`sync.Mutex`. Every public method (`Apply`, `Get`, `Sweep`, `Snapshot`) takes
the lock, so all operations are atomic and linearizable. No secondary index is
kept; expiration is evaluated by scanning the map.

**Expiration.** An entry is expired exactly when `ExpiresAt <= now`. Expiry is
lazy: entries are removed only when an operation carrying time (`Apply`,
`Get`, `Sweep`) advances the clock, at which point all due entries are deleted
in one pass and their value bytes are subtracted from the live total.

**Batch transactions.** `Apply` runs in three phases:

1. Structural validation of every op in input order (key charset/length,
   kind-specific field rules, `ExpiresAt > Batch.Now`) without touching state.
2. Monotonic-time check (`Batch.Now >= Snapshot.Now`, else `ErrTime`).
3. Execution on an isolated candidate copy of the map: expire due entries,
   then apply ops sequentially, allocating one revision per `Put` from
   `NextRevision` (starting at 1). `Delete`/`Touch` of a missing key fail with
   `ErrNotFound`; final entry-count and total-byte limits are checked after
   all ops (`ErrCapacity`).

Any failure discards the candidate: expiration, time, generation, state, and
revision allocation all roll back. On commit, `Now` advances and generation
increments once if anything expired or the batch had at least one op.

**Time.** The store starts at time 0 and only moves forward. `Get` and `Sweep`
atomically expire due entries, advance `Now`, and bump the generation once
only when at least one entry expired.

**Capacity.** Limits are `MaxEntries` (live key count) and `MaxTotalBytes`
(sum of live value bytes), checked after a batch's ops complete. `MaxValueBytes`
bounds each individual value at validation time.

**Ownership.** All `[]byte` values are deep-copied on the way in (`Apply`) and
on the way out (`Get`, `Snapshot`), so callers can never alias store state.

**Complexity.** Let *n* be the number of live entries and *m* the ops in a
batch. `Apply` is O(n + m) (candidate copy plus op execution), `Get`/`Sweep`
are O(n) (expiry scan), and `Snapshot` is O(n log n) (sorting by key). Expiry
scans are O(n); expired-key results are sorted in O(e log e) for *e* expirations.

## Usage

```sh
go test ./...
go run ./cmd/demo
```
