# expirytable274

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

## Design notes

### Index

Entries live in a single `map[string]Entry` keyed by the validated key, guarded by one `sync.Mutex`. Every public method (`Apply`, `Expire`, `Snapshot`, `Stats`, `Clone`) takes the lock, so all of them are linearizable and safe for concurrent use. `Snapshot` and `Expire` return entries sorted by key for deterministic output; the returned slices are freshly allocated and never alias internal state.

### Candidate transactions

`Apply` runs in three phases:

1. **Structural preflight** — `validateBatch` (shared with `ValidateBatch`) checks `Now >= 0`, op kinds, key charset/length, and `ExpiresAt > Now` for Put/Touch, without reading mutable state.
2. **Time check** — `Now < current` fails with `ErrTime` before any mutation.
3. **Candidate execution** — a copy of the live map is built, entries with `ExpiresAt <= Now` are dropped (closed boundary), and Put/Touch/Delete run in order, allocating monotonically increasing revisions. The final size is checked against `MaxEntries`.

Any error (`ErrNotFound`, `ErrCapacity`, …) simply discards the candidate, so evictions, the logical clock, and the revision counter all roll back together. A successful non-empty batch bumps `generation` exactly once; an empty batch changes nothing.

### Ownership

`Clone` copies the map, both logical clocks (`generation`, `nextRevision`), and `now` into a brand-new `Table` with its own mutex. No slices or maps are shared, so later writes to either table are invisible to the other. `Snapshot`/`Stats` likewise return values detached from internal storage.

### Complexity

- `Apply`: O(E + B) where E = live entries (candidate copy + expiry sweep) and B = batch size.
- `Expire`: O(E + K log K) where K = expired entries (sorting the result).
- `Snapshot`: O(E log E) due to key sorting; `Stats`: O(1) besides locking.
- `Clone`: O(E). `ValidateBatch`: O(B), no state access.
