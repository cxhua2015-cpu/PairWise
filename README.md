# topologygraph278

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## Design notes

### Indexing

The graph keeps four indexes under a single `sync.RWMutex`: a node set, an
edge set keyed by `Edge{From, To}`, and forward/reverse adjacency maps
(`from -> {to}`, `to -> {from}`). The edge set gives O(1) existence checks;
the dual adjacency maps make `DeleteNode` cascade and cycle detection
proportional to the affected neighborhood instead of the whole graph.

### Candidate transactions

`Apply` first runs the shared structural validation (`ValidateBatch`, no
state reads), then executes the batch speculatively on a private *candidate*
copy of the state. Semantic failures (`ErrExists`, `ErrNotFound`,
`ErrCycle`) abort immediately; node/edge capacity is checked only against
the final candidate state. Any failure simply discards the candidate, so a
failed batch is a full rollback with zero mutation of live state. On success
the candidate maps are swapped in and the generation increments exactly
once (empty batches leave it unchanged).

### Ownership

All returned values are detached: `Snapshot` builds freshly sorted slices,
`Stats` reads counts under one lock hold (linearizable), and `Clone`
deep-copies every map and nested adjacency set plus the logical clock, so
clones and originals can never alias. Mutating a returned slice or applying
batches on a clone never affects the source graph.

### Complexity

- `Apply`: structural validation O(batch); speculative execution O(batch +
  state) for the candidate copy; cycle check per `AddEdge` is O(V + E) DFS.
- `DeleteNode`: O(degree) cascade via the reverse index.
- `Reachable`: O(V + E) DFS on a read-locked consistent view.
- `Snapshot`: O(V log V + E log E) stable sort; `Stats`/`ValidateBatch`: O(1)/O(batch).
