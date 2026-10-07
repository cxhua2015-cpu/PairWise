# topologygraph403

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## Design notes

- **Indexes**: the graph keeps three in-memory indexes guarded by a single `sync.RWMutex`: a node set, an edge set keyed by `Edge{From, To}`, and an adjacency map `from -> set(to)` used for O(V+E) cycle checks and `Reachable` DFS. All reads (`Reachable`, `Snapshot`, `Stats`, `Clone`) take the read lock; `Apply` takes the write lock.
- **Candidate transactions**: `Apply` first runs the shared side-effect-free structural validation (`ValidateBatch`), then clones the three indexes into candidate maps, applies ops in order, and checks node/edge capacity only against the final candidate state. On any error the candidate is discarded (full rollback); on success the candidate is swapped in and `generation` increments exactly once per non-empty batch.
- **Ownership**: all returned slices (`Snapshot`) and the `Clone` result are freshly allocated deep copies; no internal map or slice is ever aliased to callers or between clones, so mutations of a clone never affect the original.
- **Complexity**: validation is O(batch); `Apply` is O(V+E) to build the candidate plus O(batch) op work and O(V+E) per `AddEdge` cycle check; `Reachable` is O(V+E); `Snapshot` is O(V log V + E log E) for stable sorting; `Stats` is O(1); `Clone` is O(V+E).
