# topologygraph243

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## Design notes

### Indexing

The graph keeps two hash indexes guarded by a single `sync.RWMutex`: a
`map[string]struct{}` node set and a `map[Edge]struct{}` edge set keyed by the
`(From, To)` pair. Lookups, existence checks and duplicate detection are O(1).
Reachability and cycle detection run an iterative DFS over the edge index.
`Snapshot` copies both indexes and stably sorts nodes lexicographically and
edges by `(From, To)`, so results are deterministic and fully detached from
internal state.

### Candidate transactions

`Apply` first runs the shared side-effect-free structural check
(`ValidateBatch`), then builds a private *candidate* copy of the node and edge
maps under the write lock. All ops replay against the candidate; node/edge
capacity limits are checked only on the final candidate state. On any error the
candidate is discarded (full rollback, generation untouched); on success the
candidate is swapped in and `Generation` increments exactly once per non-empty
batch. Empty batches succeed without advancing the clock.

### Ownership

All returned values (`Snapshot`, `Stats`, `Result`) are freshly allocated and
never alias internal maps. `Clone` deep-copies both indexes plus the logical
clock under the read lock, producing a fully independent graph: subsequent
mutations on either graph are invisible to the other. `ValidateBatch` reads
only `Options` and never touches or mutates graph state.

### Complexity

- `ValidateBatch`: O(k·L) for k ops of name length L, no state access.
- `Apply`: O(V + E) to build the candidate, plus per-op O(1) node ops and
  O(V + E) worst case per `AddEdge` cycle check; final capacity check O(1).
- `DeleteNode`: O(E) to remove incident edges.
- `Reachable`: O(V + E) DFS under the read lock.
- `Snapshot`: O(V log V + E log E) for the stable sorts.
- `Stats`: O(1). `Clone`: O(V + E).
