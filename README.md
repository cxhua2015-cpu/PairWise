# topologygraph238

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

- `trustgraph.go` — core state, atomic batch transactions, `Reachable`, `Snapshot`.
- `validation.go` — shared name grammar and side-effect-free `ValidateBatch`; `Apply` reuses exactly these structural semantics before touching state.
- `stats.go` — `Stats`, a linearizable summary read under the same lock as the data.
- `clone.go` — `Clone`, a deep copy preserving the logical clock (`generation`).

## Indexes

The graph keeps three in-memory indexes guarded by one `sync.RWMutex`:

- `nodes`: node set, `map[string]struct{}`.
- `edges`: edge set keyed by ordered `(From, To)` pair for O(1) existence checks.
- `out`: forward adjacency index `From -> {To}` used by cycle detection and `Reachable`. Inbound edges are derived by scanning `out`, so memory stays proportional to `|V| + |E|`.

## Candidate transactions

`Apply` first runs `ValidateBatch` (pure structural check, no state reads), then replays the ops on a **candidate**: private copies of the three indexes. Semantic errors (`ErrExists`, `ErrNotFound`, `ErrCycle`) abort immediately; node/edge capacity is checked only once at the end of the batch (`ErrCapacity`). Any failure simply discards the candidate, so rollback is free and no partial state is ever observable. On success the candidate maps are swapped in and `generation` increases exactly once; empty batches leave the clock untouched.

## Ownership

Every value crossing the API boundary is fully owned by the caller: `Snapshot` returns freshly allocated, stably sorted slices (nodes lexicographic; edges by `(From, To)`), and `Clone` deep-copies all indexes so the two graphs can never alias. Mutating a returned snapshot or a clone never affects the source graph.

## Complexity

- `Apply`: O(|V| + |E|) to build the candidate, plus O(|V| + |E|) per `AddEdge` for the cycle reachability check, plus O(out-degree scan) for `DeleteNode` cascading.
- `Reachable`: O(|V| + |E|) BFS/DFS on a read-locked consistent view.
- `Snapshot`: O(|V| log |V| + |E| log |E|) for stable sorting.
- `Stats`: O(1). `ValidateBatch`: O(total name bytes), no state access.
- `Clone`: O(|V| + |E|).

Writers serialize on the mutex; readers (`Reachable`, `Snapshot`, `Stats`, `Clone`) run concurrently under the read lock.
