# readyqueue265

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## Design notes

### Indexing

The queue keeps a single ownership map `map[string]Item` keyed by ID, guarded
by one `sync.Mutex`. There is no separate heap: `Pop` and `Snapshot` materialize
a sorted view on demand using the canonical order (Priority desc, ReadyAt asc,
ID asc). With `MaxItems` bounded by configuration this keeps the structure
simple and exactly consistent with the map — no index can drift out of sync.

### Candidate transactions

`Apply` is a two-phase candidate transaction:

1. **Structural pre-check** — shared with `ValidateBatch`, it validates the
   batch (non-negative time, known kinds, ID charset/length, no payload on
   Cancel) without reading or mutating any state.
2. **Staged mutation** — ops execute in order against a *copy* of the item map
   and a local revision counter. Final capacity is checked only at the end.
   On success the copy is swapped in and `generation`, `nextRevision` and
   `now` are committed; on any failure the originals are untouched, so time,
   state and revision roll back for free.

A non-empty successful batch increments `generation` exactly once; an empty
batch is a no-op.

### Ownership

All returned slices (`Pop`, `Snapshot`) and the `Clone` queue are deep copies
that share no backing memory with the queue. `Clone` also copies the logical
clocks (`generation`, `nextRevision`, `now`) and the option limits, so the
clone evolves fully independently. Every public method takes the mutex, giving
linearizable `Apply`, `Pop`, `Snapshot`, `Stats` and `Clone`.

### Complexity

Let `n` be the number of stored items and `b` the batch size.

- `Apply`: `O(n + b)` — one map copy plus per-op `O(1)` map operations.
- `Pop`: `O(n log n)` — full sort of the candidate view, then `O(k)` deletes.
- `Snapshot`: `O(n log n)`; `Stats`: `O(1)`; `Clone`: `O(n)`.
- `ValidateBatch`: `O(b · idlen)`, no state access.
