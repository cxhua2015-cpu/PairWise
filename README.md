# windowcounter

Concurrency-safe, exact sliding-window counter registry driven by explicit
time. Go 1.22+, standard library only. See `SPEC.md` for the contract.

## Design

- **Indexing**: a single `map[string]*keyState` guarded by one `sync.Mutex`.
  Each `keyState` keeps its retained events as a chronologically appended
  `[]event{at, amount}` plus a running `sum`, so per-key reads are O(1) and
  appends are amortized O(1). A registry-wide `totalEv` counter tracks the
  live event count without iterating the map.
- **Window cleanup**: an event is retained exactly while `at > now-Window`
  (computing `now-Window` cannot overflow because `now >= 0` and
  `Window > 0`; when `now < Window` the cutoff is negative and nothing
  expires). Expiration filters each key's slice in place, recomputes the
  key sum from the retained events, drops empty keys, and adjusts `totalEv`.
  It runs inside `Apply` (on the candidate state), `Get`, and `Sweep`.
- **Transactions**: `Apply` first structurally validates every delta in input
  order without reading state, then checks time monotonicity
  (`now >= Snapshot.Now`, else `ErrTime`). It deep-clones the live state,
  expires old events on the clone, and applies deltas sequentially at
  `Batch.Now`. Any failure (`ErrUnderflow`, `ErrOverflow`, `ErrCapacity`)
  simply discards the clone, so expiration, time, generation, sums, and
  events never leak from a failed batch. On success the clone is swapped in,
  `Now` commits, and generation increments once if anything expired or at
  least one delta was applied. `Get`/`Sweep` mutate in place under the same
  lock and bump generation only when at least one event actually expires.
- **Arithmetic safety**: the live sum of a key may never go negative
  (`ErrUnderflow`); positive addition is guarded with
  `sum > math.MaxInt64-amount` (`ErrOverflow`). Negative-side int64 overflow
  is impossible because sums are kept nonnegative.
- **Capacity**: `MaxKeys` and `MaxEvents` are enforced only on the *final*
  candidate state after all deltas, so a batch may expire old data to make
  room for its replacements. Violations return `ErrCapacity` and roll back.
- **Complexity**: `Apply` is O(K + E + D + T log T) for K live keys, E live
  events, D deltas, and T touched keys (sorting `Result.Counts`). `Get` and
  `Sweep` are O(K + E). `Snapshot` is O(K log K + E) for sorting and copying.
  All returned slices are freshly allocated and ownership-isolated.

## Usage

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
