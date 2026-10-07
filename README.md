# topologygraph418

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## Design notes

### Indexes

The graph keeps four maps behind a single `sync.RWMutex`: the node set, the
edge set, a forward adjacency index (`adj`), and a reverse adjacency index
(`rev`). The forward index drives cycle checks and `Reachable`; the reverse
index makes `DeleteNode` cascade to incoming edges without a full scan.

### Candidate transactions

`Apply` first runs the shared structural validation (`ValidateBatch`, no
state access), then clones the live maps into a candidate view and replays
every op against it. Node/edge capacity is checked only on the final
candidate state. Any error discards the candidate, so a failed batch is a
complete rollback and `generation` never moves; a non-empty successful batch
swaps the candidate in and bumps `generation` exactly once. Empty batches
succeed without changing the generation.

### Ownership

`Snapshot` builds fresh, stably sorted slices (nodes lexicographic, edges by
`(From, To)`), so callers can never alias internal state. `Clone` deep-copies
every map — including the logical clock (`generation`) — into a graph with
its own mutex; later batches on either graph are fully independent. `Stats`
and `Reachable` take the read lock, giving linearizable, consistent views
concurrent with writers.

### Complexity

- `Apply`: O(V + E) to build the candidate, plus O(V + E) worst case per
  `AddEdge` for the DFS cycle check; capacity check is O(1) at batch end.
- `DeleteNode`: O(degree) via both adjacency indexes.
- `Reachable`: O(V + E) DFS under the read lock.
- `Snapshot` / `Clone`: O(V + E), plus O(V log V + E log E) sorting for
  `Snapshot`.
- `Stats` / `ValidateBatch`: O(1) and O(batch size) respectively, with
  `ValidateBatch` performing no state reads or writes.
