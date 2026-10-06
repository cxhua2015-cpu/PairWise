# topologygraph293

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

- `trustgraph.go` — core transaction engine (`New`, `Apply`, `Reachable`, `Snapshot`) plus shared structural op validation.
- `validation.go` — `ValidateBatch`, a side-effect-free structural precheck that shares the exact same per-op semantics (`validateOp`) used by `Apply` before any state is read.
- `stats.go` — `Stats`, a linearizable count/generation summary taken under the read lock.
- `clone.go` — `Clone`, a deep copy that preserves the logical clock (`Generation`) and shares no mutable state with the original.

## Indexing

The graph keeps redundant in-memory indexes, all maintained atomically inside each committed batch:

- `nodes: map[string]struct{}` — node membership, O(1) existence checks.
- `edges: map[Edge]struct{}` — edge membership, O(1) duplicate/lookup checks.
- `out: map[string]map[string]struct{}` — adjacency (from → tos) for cycle detection and `Reachable`.
- `in: map[string]map[string]struct{}` — reverse adjacency so `DeleteNode` can cascade-delete incoming edges without scanning all edges.

## Candidate transactions

`Apply` first runs the shared structural validation (no state access), then takes the write lock and applies the ops to a private copy of the indexes (a candidate transaction). Capacity limits (`MaxNodes`/`MaxEdges`) are checked only against the final candidate state at the end of the batch. On any error the candidate is discarded — the committed state and `Generation` are untouched, giving full rollback. On success the candidate indexes are swapped in and `Generation` increments exactly once; empty batches never change the clock.

## Ownership

All public methods are safe for concurrent use: a single `sync.RWMutex` guards every field, writers are fully serialized, and readers (`Reachable`, `Snapshot`, `Stats`, `Clone`) take the read lock. Returned slices (`Snapshot`) and values (`Stats`, `Result`) are freshly allocated and never alias internal maps. `Clone` deep-copies every map, so the clone and the original evolve independently.

## Complexity

Let `B` be the batch size, `N`/`E` the node/edge counts, and `D` a node's degree.

- `Apply`: O(N + E) to snapshot the candidate indexes, plus O(B · (N + E)) worst case for per-op cycle checks (DFS over `out`).
- `AddNode`/`DeleteNode` per op: O(1) / O(D) for the cascade.
- `AddEdge`/`DeleteEdge` per op: O(N + E) cycle DFS / O(1).
- `Reachable`: O(N + E) BFS on the read-locked consistent state.
- `Snapshot`: O(N log N + E log E) for the stable sorted output.
- `Stats`: O(1); `Clone`: O(N + E).
