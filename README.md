# topologygraph298

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

## Design notes

### Indexes

The graph keeps three in-memory indexes guarded by a single `sync.RWMutex`:

- `nodes`: set of node names (`map[string]struct{}`).
- `edges`: set of directed edges keyed by `Edge{From, To}`.
- `adj`: forward adjacency list (`map[string]map[string]struct{}`) used for cycle detection and `Reachable` DFS.

All three are updated atomically at batch commit, so readers always observe a consistent triple.

### Candidate transactions

`Apply` runs in two phases:

1. **Structural preflight** — `ValidateBatch` checks kinds, name charset/length, and field placement without touching state. `Apply` and `ValidateBatch` share this exact code path.
2. **Shadow commit** — under the write lock, the batch is replayed against private copies of the three indexes (a candidate transaction). Semantic errors (`ErrExists`, `ErrNotFound`, `ErrCycle`) abort immediately; node/edge capacity is checked only once at the end against the candidate. Any failure discards the candidate, leaving the committed state untouched (full rollback). On success the candidate is swapped in and `generation` increments exactly once; empty batches never bump the clock.

### Ownership

`Snapshot` and `Stats` copy under the read lock and return freshly allocated slices/values, so callers can mutate results freely. `Clone` deep-copies every map (including the adjacency sets) plus the logical clock, giving the clone fully independent ownership — subsequent mutations on either graph never alias the other.

### Complexity

Let N = nodes, E = edges, B = batch size.

- `Apply`: O(N + E) to build the candidate, O(B · (N + E)) worst case for per-op cycle DFS, plus O(1) final capacity check.
- `ValidateBatch`: O(B · nameLen), no state access.
- `Reachable`: O(N + E) DFS under a read lock.
- `Snapshot`: O(N log N + E log E) for the stable sorted output.
- `Stats`: O(1). `Clone`: O(N + E).

## Verification

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
