# expirytable279

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

## Design notes

- **Index**: entries live in a single `map[string]Entry` keyed by the validated key, giving O(1) Put/Touch/Delete lookups. `Snapshot` and `Expire` emit entries sorted by key (canonical order), so those calls are O(n log n); `Stats` is O(1).
- **Candidate transactions**: `Apply` first runs the shared structural precheck (`ValidateBatch`: non-negative time, known kinds, key charset/length, `ExpiresAt > Now` for Put/Touch), then checks monotonic time. It clones the map into a candidate, evicts `ExpiresAt <= Now` (closed boundary), applies ops in order allocating revisions to Put/Touch, and commits only if the final size fits `MaxEntries`. Any error (including `ErrCapacity` and `ErrNotFound`) discards the candidate, rolling back evictions, time, and revisions atomically. A non-empty successful batch bumps `generation` exactly once; an empty batch leaves it unchanged but still advances time and evicts.
- **Ownership**: all returned slices (`Snapshot`, `Expire`) are freshly allocated copies, and `Clone` deep-copies the map together with the logical clocks (`now`, `generation`, `nextRevision`), so no mutable state is ever shared with callers or between tables.
- **Concurrency**: a single `sync.Mutex` serializes all public methods, making `Stats`/`Snapshot`/`Clone` linearizable against concurrent `Apply`/`Expire` transactions. Batch validation is O(batch size), eviction/expire is O(n) over live entries.
