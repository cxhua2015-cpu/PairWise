# expirytable264

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## Architecture

### Indexing

Entries live in a single `map[string]Entry` keyed by the validated key, giving O(1) average lookup for Put/Touch/Delete. A `sync.Mutex` guards the whole table, so every public method (`Apply`, `Expire`, `Snapshot`, `Stats`, `Clone`, `ValidateBatch`) is safe for concurrent use; `Snapshot` and `Expire` return entries sorted by key for deterministic output, and all returned slices are freshly allocated copies isolated from internal state.

### Candidate transactions

`Apply` first runs the shared structural precheck (`ValidateBatch`: non-negative `Now`, known kind, key charset/length, `ExpiresAt > Now` for Put/Touch), then the monotonic-time check. It clones the live map into a candidate, evicts entries with `ExpiresAt <= Now` (closed interval, same boundary as `Expire`), and replays the ops in order, assigning one revision per Put/Touch. Only if the final candidate fits `MaxEntries` does the transaction commit — entries, `now`, `nextRevision` and (for non-empty batches) a single `generation` bump are swapped in atomically. Any error (`ErrTime`, `ErrNotFound`, `ErrCapacity`) discards the candidate, rolling back evictions, time and revisions together.

### Ownership

`Clone` deep-copies the entry map and all logical clocks (`now`, `generation`, `nextRevision`) under the lock; the clone owns an entirely independent map and mutex, so subsequent transactions on either table never alias the other. `Snapshot`/`Stats` are linearizable: they are computed under the same lock that serializes transactions.

### Complexity

- `ValidateBatch`: O(total key bytes), no state access.
- `Apply`: O(n + total key bytes) for the candidate copy and replay, where n is the live entry count.
- `Expire`: O(n + k log k) for the scan and sorting the k expired entries.
- `Snapshot`/`Clone`: O(n log n) / O(n); `Stats`: O(1).
