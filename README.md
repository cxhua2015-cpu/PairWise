# expirytable404

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

## Design notes

### Index

Entries live in a single `map[string]entry` keyed by the validated key string, giving O(1) average lookup for Put/Touch/Delete. There is no secondary time index: expiry is a full scan (`ExpiresAt <= now`, closed interval), which keeps the transactional candidate pass simple and exactly O(n).

### Candidate transactions

`Apply` first runs the shared structural validation (`validation.go`), then checks the monotone clock (`Batch.Now >= table.Now`, else `ErrTime`). It builds a candidate map as a clone-on-write copy of the live entries, drops entries with `ExpiresAt <= Now`, and replays the ops in order; Put/Touch allocate revisions from a candidate counter. Only if the final candidate size fits `MaxEntries` are the candidate map, clock, and revision counter committed. Any error (`ErrNotFound`, `ErrCapacity`, ...) discards the candidate, so expirations, time, and revisions roll back together. A successful non-empty batch increments `generation` exactly once; empty batches leave it unchanged.

### Ownership

All mutable state sits behind a single `sync.RWMutex`, so every public method is concurrency-safe and `Stats`/`Snapshot` are linearizable. `Snapshot` and `Expire` return freshly allocated slices sorted by key (canonical order); callers cannot alias internal state. `Clone` copies the map and logical clocks (`now`, `generation`, `nextRevision`) under the read lock, producing a fully independent table.

### Complexity

- `New`, `Stats`: O(1).
- `ValidateBatch`: O(ops · keyLen), no state access.
- `Apply`: O(n + ops) time, O(n) scratch space for the candidate map.
- `Expire`, `Snapshot`, `Clone`: O(n log n) for `Expire`/`Snapshot` due to canonical key ordering (O(n) for `Clone`).
