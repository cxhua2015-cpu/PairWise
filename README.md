# topologygraph283

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

## Design notes

### Indexing

The graph keeps three structures under a single `sync.RWMutex`: a node set, an edge set keyed by `Edge{From, To}`, and a forward adjacency index `map[from]map[to]`. The edge set gives O(1) existence checks; the adjacency index makes cycle detection and `Reachable` a plain BFS/DFS over out-edges without scanning all edges.

### Candidate transactions

`Apply` first runs the shared structural validation (`ValidateBatch`, no state access), then takes the write lock and replays the batch against a candidate copy of the three maps. Node/edge capacity is checked only once, at the end of the batch, against the candidate. Any error (`ErrExists`, `ErrNotFound`, `ErrCycle`, `ErrCapacity`) discards the candidate, so failed batches roll back completely and never bump `Generation`. A non-empty successful batch swaps the candidate in and increments `Generation` exactly once; empty batches leave it unchanged.

### Ownership

`Snapshot` and `Clone` allocate fresh slices/maps, so callers can never mutate internal state, and a clone is fully independent of the original (including its logical clock). `Reachable` and `Stats` run under the read lock, giving a linearizable, consistent view of committed state.

### Complexity

- `ValidateBatch`: O(total name bytes), no state access.
- `Apply`: O(state size) to build the candidate, plus O(V + E) per `AddEdge` for the cycle reachability check; capacity check is O(1).
- `DeleteNode`: O(E) to detach incident edges.
- `Reachable`: O(V + E) under a read lock.
- `Snapshot`: O(V log V + E log E) for stable sorting; `Stats` is O(1); `Clone` is O(V + E).
