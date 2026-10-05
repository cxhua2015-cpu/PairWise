# dagstore

A concurrency-safe, in-memory transactional DAG store for the control plane.
Go 1.22+, standard library only. See `SPEC.md` for the full contract.

## Design

### Dual edge indexes
Edges are kept in three structures: a primary map `edgeKey{from,to} -> revision`
for O(1) existence checks, plus two adjacency indexes — `out[from]` and
`in[to]` — so reachability, cycle detection, topological sorting, and the
"node still has edges" delete check never scan the full edge set.

### Candidate transactions
`Apply` validates every op structurally first (no state reads), then forks an
isolated candidate (copies of the node map, edge map, and both adjacency
indexes; payload slices are immutable and shared). Ops execute in input order
on the candidate. Any conflict, missing object, cycle, or final capacity
failure simply discards the candidate — nodes, both edge indexes, generation,
and revision allocation roll back for free. On success the candidate maps are
swapped in under a single write lock.

### Cycle detection
The committed graph is always a DAG, so `AddEdge(from,to)` can only close a
cycle if `to` already reaches `from`. A DFS over the `out` index answers that
in O(V+E) per edge add.

### Revision and generation
Every successful op consumes one consecutive revision (starting at 1); nodes
and edges record the revision of their latest creation/update. Removed objects
consume a revision but are dropped from `ChangedNodes`/`ChangedEdges`. A
successful nonempty batch increments `generation` once; failed and empty
batches change neither counter. `Snapshot.NextRevision` is `revision+1`.

### Capacity
Node count, edge count, and total live payload bytes are checked **only after
all ops execute**, so batches may temporarily exceed limits as long as the
final state complies (e.g. add-then-delete in one batch).

### Ownership
All payloads are deep-copied on the way in (Apply) and on the way out
(Result, Snapshot), so callers and the store never share mutable memory.

### Concurrency
A single `sync.RWMutex` guards everything: writers are fully serialized,
readers (`Reachable`, `Topological`, `Snapshot`) run concurrently under the
read lock.

## Complexity (V nodes, E edges, B ops per batch)

- `Apply`: O(B) structural validation + O(V+E) candidate fork + per-op O(1)
  map work, plus O(V+E) per `AddEdge` for cycle DFS; final capacity O(V).
- `Reachable`: O(V+E) DFS.
- `Topological`: O(V log V + E) using a sorted ready-set with insertion.
- `Snapshot`: O(V log V + E log E) for stable sorted output.

## Verification

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
