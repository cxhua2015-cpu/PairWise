# topologygraph248

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

## Design notes

### Indexing

The graph keeps four maps behind a single `sync.RWMutex`: a node set, an edge set keyed by `Edge{From, To}`, and two adjacency indexes (`out` for forward traversal, `in` for reverse lookup). The adjacency indexes make cycle checks a plain graph walk and let `DeleteNode` cascade to all incident edges in O(degree) instead of scanning every edge. Empty adjacency buckets are removed eagerly so re-adding a node never sees stale entries.

### Candidate transactions

`Apply` first runs the shared structural precheck (`ValidateBatch`, no state access), then takes the write lock and applies ops in order against the live state while recording an undo log of inverse actions (each added node/edge is paired with its removal and vice versa; a cascaded `DeleteNode` records the node plus every removed edge). Any failure — existence conflicts, missing endpoints, cycle creation, or the end-of-batch capacity check — replays the log in reverse, restoring the exact prior state. Capacity limits are only evaluated once, after all ops, so a batch may temporarily exceed `MaxNodes`/`MaxEdges` mid-flight. A non-empty successful batch increments `generation` exactly once; empty or failed batches leave it unchanged.

### Ownership

All returned values are fully detached from internal state: `Snapshot` builds fresh sorted slices, `Stats` is a value copy, and `Clone` deep-copies every map (including the adjacency indexes and the logical clock) so the clone and the original can be mutated independently with zero aliasing. No internal map or slice ever escapes the mutex.

### Complexity

- `Apply`: O(V + E) worst case per batch (cycle reachability check per `AddEdge`, plus undo-log bookkeeping); structural validation is O(total name bytes).
- `ValidateBatch`: O(total name bytes), no locks, no state reads.
- `Reachable`: O(V + E) DFS under a read lock.
- `Snapshot`: O(V log V + E log E) due to stable sorting of nodes and edges.
- `Stats`: O(1) under a read lock; linearizable with concurrent writers.
- `Clone`: O(V + E) under a read lock.
