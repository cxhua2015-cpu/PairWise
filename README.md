# topologygraph433

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

- `trustgraph.go` — core types, indexes, atomic batch transactions, reachability, snapshots.
- `validation.go` — side-effect-free structural batch validation shared with `Apply`.
- `stats.go` — linearizable state statistics.
- `clone.go` — deep copy preserving the logical clock with full ownership isolation.
- `preview.go` — transaction rehearsal on a linearizable snapshot.

## Design notes

### Indexes

The graph keeps two hash indexes guarded by a single `sync.RWMutex`: a node
set (`map[string]struct{}`) and an edge set (`map[Edge]struct{}`). Adjacency
lists are not stored; they are built on demand for cycle checks and
`Reachable`, keeping mutations O(1) per op.

### Candidate transactions

`Apply` first runs structural validation without touching state, then copies
both indexes into a candidate, replays the ops against the candidate, and
checks node/edge capacity only at the end of the batch. On success the
candidate is swapped in atomically and `generation` advances exactly once
(empty batches leave it unchanged); on any failure the candidate is discarded
and the committed state is untouched, giving whole-batch rollback.

### Ownership

Every graph exclusively owns its maps. `Snapshot`, `Clone` and `Preview`
deep-copy all state, so returned slices and cloned graphs share no memory
with the receiver; callers may mutate results freely and clones evolve
independently. `Clone` preserves the logical clock (`generation`).

### Preview

`Preview` clones the receiver under one linearizable read, then runs the full
`Apply` semantics on the candidate. It returns the candidate `Result`,
`Snapshot` and `Stats` — identical to committing the same batch on that
state — while the receiver's state, generation and logical clock are
unchanged. Failures return the same error `Apply` would, with zero values.

### Complexity

- `Apply`: O(V + E) to copy indexes, plus O(ops) index updates; each
  `AddEdge` cycle check is O(V + E); capacity check is O(1) at batch end.
- `DeleteNode`: O(E) to remove incident edges.
- `Reachable`: O(V + E) BFS/DFS over an on-demand adjacency list.
- `Snapshot`: O(V log V + E log E) for stable sorting of nodes and edges.
- `Stats`: O(1). `Clone`/`Preview`: O(V + E) copy (Preview adds the batch cost).

All public methods are safe for concurrent use; writers serialize on the
mutex, readers (`Reachable`, `Snapshot`, `Stats`, `Clone`, `Preview`) share
the read lock.
