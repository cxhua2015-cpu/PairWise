# expirytable254

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## Design notes

### Indexing

Entries live in a single `map[string]Entry` keyed by the key string, guarded by one
`sync.RWMutex`. Lookups (Put/Touch/Delete, capacity counting) are O(1) per op.
`Snapshot` and `Expire` sort results by key, so their cost is O(n log n); `Stats`
is O(1). No secondary time-ordered index is kept: expiry scans the map in O(n),
which keeps the transaction path simple and exactly linearizable.

### Candidate transactions

`Apply` runs in three phases under the write lock:

1. Structural validation (shared with `ValidateBatch`, no state access).
2. Time check: `Now` must be non-negative and not below the table clock.
3. Candidate replay: entries with `ExpiresAt <= Now` are evicted into a fresh
   candidate map, ops are applied in order (Put/Touch allocate revisions,
   Touch/Delete on a missing key fail with `ErrNotFound`), and the final
   capacity is checked.

Any failure discards the candidate map, the clock and the revision counter
untouched — eviction, time and revisions roll back together. Only a fully
successful batch commits, bumping `generation` exactly once for non-empty
batches. `Expire` uses the same closed boundary (`ExpiresAt <= now`).

### Ownership

`Snapshot` returns freshly allocated, sorted slices; `Clone` copies the map,
options and logical clocks (`now`, `generation`, `nextRevision`) into an
independent `Table`. Mutating a clone, a snapshot, or a returned `[]Entry`
never affects the original — there is no shared backing memory.

### Complexity

- `Apply`: O(n + m) for n entries and m ops, plus O(n) candidate copy.
- `Expire`: O(n log n) due to sorted output.
- `Snapshot` / `Clone`: O(n log n) / O(n).
- `Stats`: O(1). Memory: O(n).
