# topologygraph253

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

- `trustgraph.go` — core engine: options, `Apply` atomic batches, `Reachable`, `Snapshot`.
- `validation.go` — `ValidateBatch`: pure structural pre-check (kind, field placement, name charset/length, self-loops) shared by `Apply`; never reads or mutates graph state.
- `stats.go` — `Stats`: linearizable node/edge/generation summary under the read lock.
- `clone.go` — `Clone`: deep copy preserving the logical clock (`generation`) with fully independent ownership.

## Indexing

The graph keeps three indexes under a single `sync.RWMutex`:

- `nodes`: set of node names.
- `edges`: set of `Edge{From, To}` pairs for O(1) existence checks.
- `out`: adjacency map `from -> set(to)` used by cycle detection and `Reachable`.

## Candidate transactions

`Apply` first runs the shared structural validation, then under the write lock builds a *candidate* state (copies of the three indexes) and replays the ops in order. Semantic errors (`ErrExists`, `ErrNotFound`, `ErrCycle`) abort immediately; node/edge capacity is checked only once against the final candidate state. Any failure discards the candidate — the committed state is untouched, so rollback is free. On success the candidate maps replace the committed ones and a non-empty batch increments `generation` exactly once. Cycle prevention checks whether the new edge's target already reaches its source via DFS over the candidate adjacency.

## Ownership

All returned slices (`Snapshot`) and maps (`Clone`) are freshly allocated; callers can mutate them without affecting the graph. `Clone` copies every index and the generation counter, so subsequent writes to either graph never alias the other.

## Complexity

- `Apply`: O(V + E) to copy the candidate state, plus O(V + E) worst-case cycle check per `AddEdge`.
- `Reachable`: O(V + E) DFS over the adjacency index.
- `Snapshot`: O(V log V + E log E) for the stable sorted output.
- `Stats`: O(1). `Clone`: O(V + E).
