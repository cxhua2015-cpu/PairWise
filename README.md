# topologygraph223

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## Architecture notes

### Indexes

The graph keeps three in-memory indexes guarded by a single `sync.RWMutex`:

- `nodes`: `map[string]struct{}` for O(1) node membership.
- `edges`: `map[Edge]struct{}` for O(1) edge membership.
- `adj`: `map[string]map[string]struct{}` adjacency sets, used by cycle
  detection and `Reachable` so graph traversal only touches reachable
  neighbors instead of scanning the full edge set.

### Candidate transactions

`Apply` first runs the side-effect-free structural validation shared with
`ValidateBatch` (`validation.go`), then replays the batch against a private
*candidate* copy of the maps. Existence, cycle, and final node/edge capacity
checks all run on the candidate. On any error the candidate is discarded and
the committed state is untouched (full rollback); on success the candidate
maps are swapped in and `generation` advances exactly once for a non-empty
batch. Empty batches leave the generation unchanged.

### Ownership

`Snapshot` and `Stats` (`stats.go`) are linearizable: they take the read lock
and reflect one consistent committed state. `Snapshot` returns freshly
allocated, stably sorted slices (nodes lexicographic; edges by `(From, To)`),
so callers cannot mutate internal state. `Clone` (`clone.go`) deep-copies all
maps and the logical clock (`generation`) under the read lock; the clone
shares no memory with the original and evolves independently.

### Complexity

Let `B` be batch size, `N` nodes, `E` edges.

- `Apply`: structural validation `O(B)`; candidate copy cost `O(N + E)` to
  snapshot plus `O(B · (N + E))` worst case for per-op cycle BFS; final
  capacity check `O(1)`.
- `ValidateBatch`: `O(B)`, reads no graph state.
- `Reachable` / cycle check: `O(N + E)` BFS over the adjacency index.
- `Snapshot`: `O(N log N + E log E)` due to stable sorting.
- `Stats`: `O(1)`. `Clone`: `O(N + E)`.
