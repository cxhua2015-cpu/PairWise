# topologygraph418

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## Design notes

### Indexing

The graph keeps two hash indexes guarded by a single `sync.RWMutex`:
`nodes map[string]struct{}` for O(1) node membership and
`edges map[Edge]struct{}` for O(1) edge lookup and duplicate detection.
Cycle checks and `Reachable` run an iterative DFS over the edge index
(adjacency is scanned, not materialized), costing O(V + E) per query.

### Candidate transactions

`Apply` first runs the shared side-effect-free structural validation
(`ValidateBatch`), then clones the two indexes into candidate maps under
the write lock and replays every op against them. Node/edge capacity is
checked only once, at the end of the batch. Any failure (`ErrExists`,
`ErrNotFound`, `ErrCycle`, `ErrCapacity`) simply discards the candidate,
so the committed state is untouched and the batch is fully rolled back.
On success the candidate maps are swapped in and a non-empty batch bumps
`generation` exactly once; empty batches leave the clock unchanged.

### Ownership

All returned values (`Snapshot`, `Stats`, `Result`) are built from freshly
allocated slices/values, so callers can never alias internal state.
`Clone` deep-copies both indexes and the logical clock into a new `Graph`
with its own lock — the clone and the original share no memory and evolve
independently.

### Complexity

- `Apply`: O(k · (V + E)) worst case for k ops (per-`AddEdge` cycle DFS),
  plus O(V + E) to seed the candidate state.
- `Reachable`: O(V + E) DFS under a read lock on a consistent snapshot.
- `Snapshot`: O(V log V + E log E) for the stable sort of nodes and edges.
- `Stats`: O(1); `Clone`: O(V + E); `ValidateBatch`: O(k · name length),
  with no state access.
