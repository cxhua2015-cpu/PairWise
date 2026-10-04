# windowcounter

Concurrency-safe, exact sliding-window counter registry driven by explicit
time, implemented in Go 1.22+ with only the standard library. See `SPEC.md`
for the full contract; the public API lives in `windowcounter/windowcounter.go`.

## Design

- **Index**: a single `map[string]*keyState` guarded by one `sync.Mutex`.
  Each key holds its live events (`[]event{at, amount}`) plus a running
  `sum`, so reads and per-delta checks are O(1) in the key's event count.
- **Window cleanup**: an event is retained exactly while `at > now-Window`.
  Since `now` and `Window` are nonnegative `int64`, `now-Window` cannot
  overflow; when `now < Window` the cutoff is negative and nothing expires.
  Expiration compacts each key's slice in place, recomputes the sum from
  survivors, and deletes keys with no retained events.
- **Transactions**: `Apply` validates the whole batch structurally before
  touching state, then checks time monotonicity, then deep-clones the key
  map into an isolated candidate. Expiration and all deltas (applied in
  input order at `Batch.Now`, one distinct event each) run on the candidate;
  any underflow, overflow, or final capacity failure simply discards it, so
  expiration, time, generation, sums, and events never leak. On success the
  candidate is swapped in, `Now` commits, and generation increments once if
  anything expired or at least one delta was applied.
- **Arithmetic safety**: positive deltas are checked against
  `math.MaxInt64 - sum` before adding (`ErrOverflow`); any negative running
  sum is rejected (`ErrUnderflow`), which also makes `math.MinInt64` deltas
  safe because sums are always nonnegative.
- **Capacity**: `MaxKeys` (keys with retained events) and `MaxEvents`
  (total retained events) are enforced only on the final post-batch state,
  so a batch may expire old data to make room for its replacements.
- **Complexity**: `Apply` is O(K + E + D·log D) for K live keys, E live
  events, and D deltas (clone + expiry scan + delta application + sorting
  touched keys); `Get`/`Sweep` are O(K + E) due to whole-registry expiry;
  `Snapshot` is O(K log K) for sorted output. All returned slices are
  freshly allocated and ownership-isolated.

## Verification

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
