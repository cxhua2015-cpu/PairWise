# topologygraph258

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

- `trustgraph.go` — core state, atomic `Apply`, `Reachable`, `Snapshot`.
- `validation.go` — `ValidateBatch`, the shared structural preflight (kind, field, and name rules) used by both `ValidateBatch` and `Apply`.
- `stats.go` — `Stats`, a linearizable point-in-time summary.
- `clone.go` — `Clone`, a deep copy that preserves the logical clock.

## Design notes

**Indexing.** Nodes live in a `map[string]struct{}` and edges in a `map[Edge]struct{}`, so existence checks and deletes are O(1). No separate adjacency index is maintained: reachability walks the edge set directly, which keeps the candidate-transaction copy cheap and the invariants in exactly one place.

**Candidate transactions.** `Apply` first runs the shared structural preflight (no state reads), then takes the write lock and applies the batch to private copies of the node/edge maps. Node/edge capacity is checked only against the final candidate state; any failure (`ErrExists`, `ErrNotFound`, `ErrCycle`, `ErrCapacity`) simply discards the candidate, giving full rollback. On success the candidate is swapped in and `generation` increments exactly once (empty batches leave it unchanged).

**Ownership.** All returned values (`Snapshot`, `Stats`, `Result`, `Clone`) are built from freshly allocated slices/maps, so callers can never alias or mutate internal state. `Clone` copies both maps and the generation counter under the read lock; subsequent writes to either graph are fully independent.

**Complexity.** With N nodes, E edges, and B ops per batch: `Apply` is O(N + E + B·(N + E)) worst case (copy plus per-op cycle scan), `Reachable` is O(N + E), `Snapshot` is O(N log N + E log E) for stable sorting, `Stats` is O(1), and `Clone` is O(N + E). All public methods are safe for concurrent use via a single `sync.RWMutex`.
