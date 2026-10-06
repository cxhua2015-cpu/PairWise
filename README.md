# topologygraph293

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

## Design notes

### Indexes

The graph keeps three in-memory indexes guarded by a single `sync.RWMutex`:

- `nodes`: set of node names.
- `edges`: set of `Edge{From, To}` pairs for O(1) existence checks.
- `out`: adjacency map `from -> set(to)` used by cycle detection and `Reachable`.

### Candidate transactions

`Apply` first runs the shared structural validation (`validateBatch`, also used by `ValidateBatch`) without touching state. Under the write lock it then builds a candidate copy of the three indexes, replays the ops in order (existence checks, cascade delete on `DeleteNode`, cycle check via DFS on each `AddEdge`), and only at the end enforces the node/edge capacity limits. Any failure discards the candidate, so the committed state is untouched and the generation is not bumped. On success the candidate indexes are swapped in and the generation increases exactly once per non-empty batch.

### Ownership

`Snapshot` and `Clone` allocate fresh maps/slices under the read lock, so returned data shares no memory with the live graph. `Clone` also copies the generation (logical clock), producing a fully independent graph.

### Complexity

Let N = nodes, E = edges, K = ops in a batch.

- `Apply`: O(N + E) to build the candidate plus O(K · (N + E)) worst case for per-op cycle DFS; capacity check is O(1).
- `Reachable` / cycle check: O(N + E) DFS.
- `Snapshot`: O(N log N + E log E) for stable sorting.
- `Stats`: O(1). `Clone`: O(N + E).
