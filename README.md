# topologygraph253

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## Implementation notes

### Indexing

The graph keeps two hash indexes guarded by a single `sync.RWMutex`:
`nodes map[string]struct{}` for O(1) node membership and
`edges map[Edge]struct{}` for O(1) edge membership. Reachability and
cycle checks build an adjacency list on demand from the edge index;
no derived index is persisted, so there is nothing to keep in sync.

### Candidate transactions

`Apply` first runs the shared structural validation (`ValidateBatch`,
no state access), then copies the node/edge indexes into a candidate
view and replays the ops in order against it. Capacity limits are
checked only against the final candidate state; on any error the
candidate is discarded and the committed state is untouched (full
rollback). On success the candidate is swapped in and a non-empty
batch increments `generation` exactly once. `ValidateBatch`,
`Stats`, and `Clone` share the same lock and semantics, so preflight,
transactions, statistics, and cloning are mutually consistent.

### Ownership

All returned slices (`Snapshot`) are freshly allocated and sorted
(nodes lexicographically, edges by `(From, To)`), and `Clone` deep-copies
both indexes plus the logical clock, so callers can never mutate or
alias internal state. `Reachable`/`Snapshot`/`Stats`/`Clone` run under
the read lock and observe a consistent committed state.

### Complexity

Let N = nodes, E = edges, K = ops in a batch. `Apply` costs O(N + E)
to build the candidate plus O(K·(V + E)) worst case for per-edge cycle
checks (DFS per `AddEdge`). `Reachable` is O(V + E). `Snapshot` is
O(N log N + E log E) for stable sorting. `Stats` is O(1); `Clone` is
O(N + E).
