# expirytable229

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

- `heartbeat.go` — core types, constructor, `Apply`/`Expire`/`Snapshot`.
- `validation.go` — `ValidateBatch`: complete structural pre-check shared by `Apply`.
- `stats.go` — `Stats`: linearizable state summary.
- `clone.go` — `Clone`: deep copy preserving logical clocks.

## Index

Entries live in a `map[string]Entry` keyed by the validated key string, giving O(1) average lookup for Put/Touch/Delete. A single `sync.Mutex` guards all state, so every public method is safe for concurrent use and each `Apply` is atomic.

## Candidate transactions

`Apply` first runs the shared structural validation (`ValidateBatch`) without touching state, then checks the monotonic clock (`ErrTime`). It builds a candidate map from live entries, evicting `ExpiresAt <= Now` (closed interval), and replays the ops in order, allocating one revision per Put/Touch. Only if the final size fits `MaxEntries` does the candidate commit; any error (`ErrNotFound`, `ErrCapacity`, …) discards the candidate together with evictions, the clock advance and the allocated revisions. A non-empty successful batch bumps `generation` exactly once; empty batches leave it unchanged. `Expire` uses the same closed-interval boundary.

## Ownership

`Snapshot` and `Expire` return freshly allocated slices, and `Clone` copies the entry map, so callers can never alias or mutate internal state; the clone shares nothing with the original but preserves `now`, `generation` and `nextRevision`.

## Complexity

- `Apply`: O(E + B) time, O(E) scratch space for the candidate map (E = live entries, B = batch size).
- `Expire`: O(E + K log K) where K = expired entries (sorted output).
- `Snapshot`/`Clone`: O(E log K) / O(E); `Stats`: O(1).
- `ValidateBatch`: O(B), no state access.
