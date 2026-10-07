# expirytable419

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## Design notes

### Index

Entries live in a single `map[string]Entry` keyed by the validated key, so
Put/Touch/Delete lookups are O(1). There is no secondary time index: expiry
scans the map and drops entries with `ExpiresAt <= now` (closed interval),
which keeps the transaction path simple and exactly consistent with `Expire`.

### Candidate transactions

`Apply` runs in three phases under one mutex:

1. **Structural precheck** (`ValidateBatch`, side-effect free): non-negative
   `Now`, known op kinds, keys matching `[a-z0-9-_]` within `MaxKeyBytes`,
   and Put/Touch deadlines strictly greater than `Now`. This happens before
   any state is read, so a malformed batch fails with `ErrInvalidInput` even
   when the clock would also be rejected.
2. **Time check**: `Now` must not move backwards (`ErrTime`).
3. **Candidate replay**: entries with `ExpiresAt <= Now` are evicted into a
   fresh candidate map, ops execute in order (Put/Touch allocate revisions),
   and the final size is checked against `MaxEntries`. The candidate, logical
   clock, generation and revision counter are committed only on success, so
   any error (`ErrNotFound`, `ErrCapacity`) rolls back evictions, time and
   revisions atomically. Empty batches never bump the generation.

### Ownership

All public methods take the table mutex, making every operation linearizable.
`Snapshot` and `Expire` return freshly allocated, key-sorted slices detached
from internal storage. `Clone` deep-copies the entry map and logical clocks
(generation, next revision, now) into a table with its own lock; the two
tables share no mutable state.

### Complexity

- `New`, `Stats`: O(1).
- `ValidateBatch`: O(ops · key length), no state access.
- `Apply`: O(entries + ops) for the candidate copy and replay.
- `Expire`: O(entries + k log k) for the scan and sorted result.
- `Snapshot`, `Clone`: O(entries log entries) / O(entries) respectively.
