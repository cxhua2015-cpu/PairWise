# topologygraph268

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## Design notes

### Indexes

State is held in two hash indexes guarded by a single `sync.RWMutex`:
`nodes map[string]struct{}` for O(1) node membership and
`edges map[Edge]struct{}` for O(1) edge membership. Cycle checks and
`Reachable` run an iterative DFS over the edge index; `Snapshot` sorts nodes
lexicographically and edges by `(From, To)` for a stable canonical order.

### Candidate transactions

`Apply` first runs the shared structural validation (`ValidateBatch`, no state
reads), then takes the write lock and applies every op to a private candidate
copy of the node/edge maps. Any mid-batch failure (exists/not-found/cycle)
discards the candidate; node/edge capacity is checked only once at the end of
the batch, and on success the candidate is swapped in atomically and
`generation` increments exactly once. Empty batches are no-ops and never bump
the clock.

### Ownership

All returned slices (`Snapshot`) and the `Clone` graph are fully independent
deep copies: mutating them never aliases internal state, and `Clone` preserves
the logical clock (`generation`) while sharing no maps with the source.
`Stats` is computed under the read lock, so it is linearizable with concurrent
transactions.

### Complexity

- `Apply`: O(V + E) to build the candidate, plus O(V + E) per `AddEdge` cycle
  check in the worst case; final capacity check is O(1).
- `Reachable`: O(V + E) DFS on a consistent read-locked snapshot.
- `Snapshot` / `Clone`: O(V log V + E log E) sorting / O(V + E) copying.
- `ValidateBatch`: O(number of ops), no state access.
- `Stats`: O(1).
