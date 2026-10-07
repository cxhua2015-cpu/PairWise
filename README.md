# expirytable409

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

## Design notes

### Index

Entries live in a single `map[string]Entry` keyed by the validated key, giving O(1) average lookup for Put/Touch/Delete. `Snapshot` and `Expire` materialize sorted-by-key slices so results are deterministic and fully detached from internal state. A single `sync.Mutex` guards the whole table, which makes every public method (Apply, Expire, Snapshot, Stats, Clone, ValidateBatch) linearizable.

### Candidate transactions

`Apply` never mutates live state speculatively. It first runs the shared structural validation (`ValidateBatch`), then checks monotonic time, then builds a candidate map: entries with `ExpiresAt <= Now` are dropped (closed interval), and Put/Touch/Delete replay in order, with Put/Touch allocating revisions from a candidate counter. Only after the final capacity check passes are the candidate map, clock, and revision counter committed; any error (not found, capacity, time) rolls everything back, including the expirations. A non-empty successful batch bumps `generation` exactly once; empty batches leave it unchanged.

### Ownership

All returned slices and the `Clone` result are deep copies: callers can mutate them freely, and a clone shares no memory with its source while preserving the logical clocks (`now`, `generation`, `nextRevision`). `Stats` and `Snapshot` are computed under the lock, so they always reflect a consistent point-in-time state even under concurrent transactions.

### Complexity

- `Apply`: O(n + m) for n live entries (candidate copy + expiry sweep) and m ops.
- `Expire`: O(n); `Snapshot`: O(n log n) due to key sorting; `Stats`: O(1).
- `Clone`: O(n); `ValidateBatch`: O(m) with no state access.
- Memory: O(n) plus one transient O(n) candidate map per `Apply`.
