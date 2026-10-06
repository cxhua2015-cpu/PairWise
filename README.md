# topologygraph233

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## Design notes

### Indexing

The graph keeps three in-memory indexes guarded by a single `sync.RWMutex`:

- `nodes`: set of node names, `map[string]struct{}`.
- `edges`: set of directed edges keyed by `Edge{From, To}`, giving O(1) existence checks.
- `adj`: forward adjacency `map[string]map[string]struct{}` used by cycle detection and `Reachable` DFS.

### Candidate transaction and rollback

`Apply` first runs the shared structural validation (`ValidateBatch`), then mutates the live indexes op-by-op while recording inverse operations on an undo log. Any failure — including the end-of-batch capacity check (`MaxNodes`/`MaxEdges`) — replays the undo log in reverse, restoring the exact prior state. A non-empty successful batch increments `generation` exactly once; empty or failed batches leave it unchanged. Cycle prevention checks, per added edge, whether `to` already reaches `from` via DFS over `adj`.

### Ownership

All returned slices (`Snapshot.Nodes`, `Snapshot.Edges`) are freshly allocated and sorted (nodes lexicographically; edges by `(From, To)`), so callers never alias internal state. `Clone` deep-copies every map — including the nested adjacency sets and the `generation` logical clock — producing a fully independent graph.

### Concurrency and complexity

Writers (`Apply`) take the exclusive lock; readers (`Reachable`, `Snapshot`, `Stats`, `Clone`) take the read lock, making every public method linearizable. `ValidateBatch` is pure and lock-free. Complexities: `AddNode`/`DeleteEdge` O(1); `DeleteNode` O(E) (cascades incident edges); `AddEdge` O(V+E) for the cycle DFS; `Reachable` O(V+E); `Snapshot`/`Clone` O(V+E) (plus sort); `Stats` O(1).
