# expirytable234

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

- `heartbeat.go` — core types, errors, `New`, `Apply`, `Expire`, `Snapshot`.
- `validation.go` — `ValidateBatch`: pure structural precheck shared by `Apply`.
- `stats.go` — `Stats`: linearizable state summary.
- `clone.go` — `Clone`: deep copy preserving logical clocks.

## Index

Entries live in a `map[string]Entry` keyed by the entry key, giving O(1)
average lookup for Put/Touch/Delete. `Snapshot` and `Expire` results are
sorted by key for deterministic output. A single `sync.Mutex` guards all
state, so every public method is safe for concurrent use and each call
observes a linearizable state.

## Candidate transactions

`Apply` first runs the shared structural validation (`ValidateBatch`), then
checks monotonic time (`Now >= table now`, else `ErrTime`). It builds a
candidate map: entries with `ExpiresAt <= Now` (closed bound) are evicted,
then Put/Touch/Delete execute in order, with Put/Touch allocating revisions.
Only if the final size fits `MaxEntries` is the candidate committed together
with the new time, revision counter, and a single generation bump (non-empty
batches only). Any error — unknown key, capacity, time — discards the
candidate, so evictions, time, and revisions roll back atomically.

## Ownership

`Snapshot`, `Expire`, and `Clone` return freshly allocated slices/maps; no
returned value aliases internal state. `Clone` copies the logical clocks
(`now`, `generation`, `nextRevision`) and every entry into an independent
table, so mutating either table never affects the other.

## Complexity

- `Apply`: O(n + m) for n live entries (candidate copy + eviction scan) and
  m ops.
- `Expire`: O(n + k log k) for k expired entries (sorting the result).
- `Snapshot`: O(n log n) due to key-sorted output.
- `Stats`: O(1). `Clone`: O(n). `ValidateBatch`: O(m), no state access.
