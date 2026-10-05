# configstack

Read `SPEC.md` and implement the package.

## Implementation notes

### Indexing

Each layer keeps its entries in a `map[string]*entryState` for O(1) key
lookup, insert and delete. Layers themselves live in an ordered slice; a
linear scan resolves a layer by name (layer counts are bounded by
`MaxLayers`, so this stays cheap and keeps ordering trivially correct).

### Ordering

The layer slice is the priority order: index 0 is the highest priority.
`Resolve` scans the slice front to back and returns the first hit.
`AddLayer` inserts at `Position`, `Move` removes the layer and re-inserts
it at `Position` in the shortened list. `Snapshot` preserves layer order
and sorts each layer's entries by key; `Result.Changed` is sorted by layer
priority then key.

### Transactions

`Apply` first structurally validates every op (kind, allowed fields, name
and key syntax, value size, position ranges) without touching state. It
then deep-copies the live layers into an isolated candidate and executes
the ops sequentially against it. Layer count, entry count and total live
value bytes are checked only once, after the final op. Any error discards
the candidate, so nothing is rolled back explicitly — the committed state,
generation and revision counters are simply never touched. A successful
nonempty batch increments the generation exactly once; each executed `Set`
allocates exactly one revision, and revisions are only consumed on commit.

### Ownership

All `[]byte` values crossing the API boundary are deep-copied: on input
(`Set`), on output (`Resolve`, `Result.Changed`, `Snapshot`). Callers may
reuse or mutate their buffers freely, and may mutate returned buffers
without affecting the stack.

### Concurrency

A single `sync.RWMutex` guards the stack. `Apply` takes the write lock for
the whole validate-execute-commit sequence, making batches atomic with
respect to other calls. `Resolve` and `Snapshot` take the read lock.

### Complexity

Let L be the layer count, E the total entry count, and B the batch size.
Validation is O(B). Execution is O(B · (L + E)) worst case (layer moves
and inserts shift the slice; the candidate clone is O(E)). The final
capacity check is O(E). `Resolve` is O(L) map lookups. `Snapshot` is
O(E log E) for per-layer key sorting.
