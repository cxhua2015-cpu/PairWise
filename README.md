# topologygraph403

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## Design

### Indexes
- `nodes`: hash set of node names, O(1) existence checks.
- `edges`: hash set keyed by `Edge{From, To}`, O(1) duplicate/lookup.
- `adj`: adjacency map `from -> set(to)` used for cycle detection (DFS), `Reachable`, and cascade deletion on `DeleteNode`.

### Candidate transactions
`Apply` first runs the shared structural validation (`validateBatch`, also used by `ValidateBatch`), then clones the current state into a candidate graph and applies ops in order. Node/edge capacity is checked only once at the end of the batch; any error (`ErrExists`, `ErrNotFound`, `ErrCycle`, `ErrCapacity`) discards the candidate, so failures roll back atomically. A non-empty successful batch increments `generation` exactly once; empty batches leave it unchanged.

### Ownership
All returned values (`Snapshot`, `Stats`, `Clone`) are fully detached copies; mutating them never affects the graph. `Clone` deep-copies nodes, edges, adjacency, and the logical clock (`generation`), so the clone is completely independent of the original.

### Concurrency
A single `sync.RWMutex` guards all state: writers (`Apply`) take the write lock, readers (`Reachable`, `Snapshot`, `Stats`, `Clone`, `ValidateBatch`) take the read lock, giving linearizable, consistent snapshots.

### Complexity
- `Apply`: O(V + E) to build the candidate, plus O(V + E) worst-case DFS per `AddEdge` cycle check.
- `Reachable`: O(V + E) DFS under a read lock.
- `Snapshot`: O(V log V + E log E) stable sorting; `Stats`: O(1); `Clone`: O(V + E).
