# topologygraph268

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

## Design notes

### Indexes

The committed state keeps three structures in sync: a node set
(`map[string]struct{}`), an edge set (`map[Edge]struct{}`), and a forward
adjacency index (`map[string]map[string]struct{}`). The edge set gives O(1)
existence checks for `AddEdge`/`DeleteEdge`; the adjacency index drives
breadth-first reachability and cycle detection without scanning all edges.

### Candidate transactions

`Apply` first runs the shared structural precheck (`ValidateBatch`, no state
access), then clones the committed state into a candidate, replays every op
against it, and checks node/edge capacity only on the final candidate. Any
failure discards the candidate, so failed batches roll back completely and
the generation counter is untouched. A successful non-empty batch swaps the
candidate in and increments `generation` exactly once; empty batches succeed
without bumping the clock.

### Ownership

A single `sync.RWMutex` guards the state pointer and generation. Mutations
happen only on privately owned candidate copies, never in place, so readers
(`Reachable`, `Snapshot`, `Stats`, `Clone`) observe a consistent snapshot
under the read lock. `Snapshot` returns freshly allocated, stably sorted
slices; `Clone` deep-copies every map, preserving the logical clock while
sharing no memory with the original.

### Complexity

- `ValidateBatch`: O(total name bytes), no state access.
- `Apply`: O(N + E) to seed the candidate, plus O(V + E) worst case per
  `AddEdge` for the cycle check (BFS over the adjacency index).
- `Reachable`: O(V + E) BFS under the read lock.
- `Snapshot`: O(N log N + E log E) for stable sorting.
- `Stats`: O(1). `Clone`: O(N + E).
