# dagstore

A concurrency-safe, in-memory, transactional DAG store for the control plane.
Go 1.22+, standard library only. See `SPEC.md` for the normative contract.

## Design

### Dual edge index

Edges live in a primary map keyed by `(From, To)` plus two adjacency
indexes: `out[from]` (successors) and `in[to]` (predecessors). The `out`
index drives cycle checks, `Reachable`, and `Topological`; the `in` index
makes the "node has no incident edges" guard on `DeleteNode` O(1) instead of
a full edge scan. All three structures are updated together inside a batch
and committed or discarded as one unit.

### Candidate transactions

`Apply` runs in three phases under a single write lock:

1. **Structural validation** of every op in input order, without touching
   state (`ErrInvalidInput` for unknown kinds or malformed fields).
2. **Sequential execution** on an isolated candidate: shallow copies of the
   node/edge maps (node states are immutable pointers, so sharing is safe)
   and deep copies of the two adjacency indexes. Each successful op consumes
   one consecutive revision. Any conflict (`ErrConflict`), miss
   (`ErrNotFound`), or cycle (`ErrCycle`) aborts the batch; the candidate is
   simply dropped, so the live store, generation, and revision counter are
   untouched — rollback is free.
3. **Final capacity check** on the candidate: node count, edge count, and
   total live payload bytes. Failure returns `ErrCapacity` and rolls back
   exactly like any other error. Because checks happen only at the end, a
   batch may temporarily exceed limits (e.g. add a node and delete another
   later in the same batch) as long as the final state complies.

On success the candidate maps replace the live ones, the revision counter
advances to the last allocated revision, and a nonempty batch increments the
generation exactly once.

### Cycle detection

`AddEdge from->to` is rejected iff `from` is already reachable from `to`,
checked with an iterative DFS over the candidate `out` index — O(V + E) per
edge add, no recursion, no global re-validation of the graph.

### Revisions

A single monotonically increasing counter (starting at 1) is shared by nodes
and edges. Nodes and edges record the revision of their latest
creation/update. Removed objects consume a revision but do not appear in
`Result.ChangedNodes`/`ChangedEdges`. Failed batches never consume
revisions, so the sequence has no gaps from rolled-back work.

### Capacity

`MaxNodes`, `MaxEdges`, and `MaxTotalPayloadBytes` are enforced only on the
final candidate state; `MaxPayloadBytes` is a per-op structural limit. The
store tracks a running `payloadSum` updated incrementally per op.

### Ownership

Payloads are deep-copied on the way in (`AddNode`/`UpdateNode`) and on the
way out (`Result`, `Snapshot`). Mutating caller buffers or returned slices
never affects stored state.

### Complexity

- `Apply`: O(ops) structural validation; per op O(1) map work, plus
  O(V + E) per `AddEdge` cycle check; candidate setup O(V + E) per batch.
- `Reachable`: O(V + E) DFS.
- `Topological`: lexicographically smallest order via sorted ready set;
  O(V² + E) worst case with the simple re-sorting implementation.
- `Snapshot`: O(V log V + E log E) for stable sorting by name / (from, to).

### Concurrency

A single `sync.RWMutex` guards everything: `Apply` takes the write lock;
`Reachable`, `Topological`, and `Snapshot` take the read lock and never
mutate shared state. Verified with `go test -race ./...`.
