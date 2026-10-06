# topologygraph228

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## Design notes

### Indexes

`Graph` keeps three in-memory indexes guarded by a single `sync.RWMutex`:

- `nodes`: node set (`map[string]struct{}`) for O(1) membership checks.
- `edges`: edge set (`map[Edge]struct{}`) for O(1) duplicate/lookup checks.
- `out`: adjacency index (`map[string]map[string]struct{}`) so cycle
  detection and `Reachable` traverse only reachable edges instead of
  scanning the whole edge set.

### Candidate transactions

`Apply` first runs the shared structural validation from `validation.go`
(no state reads), then clones the indexes into a *candidate* state and
replays every op against it. Node/edge capacity limits are checked only
once, at the end of the batch, against the candidate. Any failure simply
discards the candidate — rollback is free and the committed state is never
touched. On success the candidate maps are swapped in and `generation` is
incremented exactly once; empty batches leave the generation unchanged.

### Ownership

All returned values (`Snapshot`, `Stats`, `Clone`) are built from freshly
allocated memory. `Clone` rebuilds every map, so the clone shares no
mutable state with the original and carries the logical clock
(`generation`) with it; subsequent mutations on either graph are fully
independent.

### Complexity

- `Apply`: O(k·(V+E)) worst case — copying the candidate costs O(V+E) and
  each `AddEdge` runs a DFS cycle check of O(V+E) over the adjacency index.
- `Reachable`: O(V+E) DFS under a read lock.
- `Snapshot`: O(V log V + E log E) for the stable sort of nodes and edges.
- `Stats`: O(1). `Clone`: O(V+E). `ValidateBatch`: O(k·L) for k ops with
  name length L, with no state access.

### Concurrency

Writers (`Apply`) take the exclusive lock; readers (`Reachable`,
`Snapshot`, `Stats`, `Clone`) share the read lock. `ValidateBatch` is
purely structural and needs no lock. Because `Apply` holds the lock for
the whole batch, each committed batch is linearizable and `Stats`/`Clone`
always observe a consistent generation-relative state.
