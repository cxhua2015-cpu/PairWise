# topologygraph413

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

- `trustgraph.go` — core types, error values, and the atomic batch transaction engine (`Apply`, `Reachable`, `Snapshot`).
- `validation.go` — side-effect-free structural precheck (`ValidateBatch`); `Apply` calls it before touching state, so both share identical structural semantics.
- `stats.go` — `Stats`, a linearizable summary read under the same lock as the state it describes.
- `clone.go` — `Clone`, a deep copy preserving the logical clock with fully independent ownership.

## Indexing

The graph keeps four maps behind a single `sync.RWMutex`:

- `nodes`: set of node names.
- `edges`: set of `Edge{From, To}` for O(1) existence checks.
- `out`: forward adjacency index (`from -> to set`) used by reachability DFS and cascade deletes.
- `in`: reverse adjacency index (`to -> from set`) so `DeleteNode` removes incoming edges without scanning all edges.

## Candidate transactions

`Apply` first runs the shared structural validation (no state access), then clones the committed state into a private *candidate*, applies every op to the candidate, and checks node/edge capacity only against the final candidate state. On any error the candidate is discarded — the committed state is never touched, giving full rollback for free. On success the candidate is swapped in and `generation` increments exactly once (empty batches leave it unchanged). Cycle prevention runs a DFS on the candidate's forward index: adding `u -> v` is rejected with `ErrCycle` when `u` is already reachable from `v`.

## Ownership

Every map in a committed state is owned exclusively by the `Graph` that committed it: `Apply` never mutates a committed state in place, `Clone` deep-copies all four maps, and `Snapshot`/`Stats` copy data into freshly allocated result slices. Returned slices therefore never alias internal state, and a clone can diverge from its origin without interference.

## Complexity

Let `B` be the batch size, `N`/`E` the node/edge counts, and `D` the out-degree of a deleted node.

- `Apply`: `O(N + E)` to build the candidate, plus `O(1)` average per node op, `O(N + E)` worst case per `AddEdge` cycle DFS, and `O(D)` per `DeleteNode` cascade; capacity check is `O(1)`.
- `ValidateBatch`: `O(B)` structural checks, no state access.
- `Reachable`: `O(N + E)` DFS under a read lock.
- `Snapshot`: `O(N log N + E log E)` for the stable sorted output.
- `Stats`: `O(1)`. `Clone`: `O(N + E)`.
