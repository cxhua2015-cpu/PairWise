# readyqueue230

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

## Design notes

### Indexing

Each item lives in exactly one `entry` allocated at enqueue time. Two indexes share that pointer: a `map[string]*entry` for O(1) ID lookup (Enqueue duplicate check, Cancel), and a `container/heap` min-heap keyed by `ReadyAt` (canonical order as tie-break) so the ready frontier is always at the root. A canonical-order heap was rejected on purpose: a not-yet-ready high-priority item would sit at the root and hide ready items behind it.

### Candidate transactions

`Apply` first runs the same structural validation as `ValidateBatch` (no state access), then checks clock monotonicity, then executes ops in order against the live state while recording an undo log (reverse of each mutation plus the starting revision counter). Any failure — duplicate ID, missing ID, or the final capacity check — replays the undo log in reverse, restoring items, `nextRevision` and the clock, so a failed batch is fully invisible. Capacity is enforced only after all ops ran, so `Cancel` + `Enqueue` in one batch can succeed at full capacity. `Pop` drains the ready candidate set from the heap, selects up to `limit` items in canonical order (Priority desc, ReadyAt asc, ID asc), and re-inserts the surplus candidates.

### Ownership

A single `sync.Mutex` guards all state; every public method (`Apply`, `Pop`, `Snapshot`, `Stats`, `Clone`, `ValidateBatch`) is one critical section, which gives linearizable semantics for concurrent callers. `Snapshot` and `Pop` return freshly allocated slices of copied `Item` values, and `Clone` re-allocates every entry and rebuilds both indexes — no returned value ever aliases internal state, and a clone shares no memory (including logical clocks `now`/`generation`/`nextRevision`, which are copied by value) with its source.

### Complexity

- `New`, `Stats`: O(1).
- `ValidateBatch`: O(ops) structural checks only, no state reads.
- `Apply`: O(ops · n) worst case — Enqueue is O(log n), Cancel is O(n) heap member removal; rollback has the same bound.
- `Pop`: O(k log n) for k ready candidates drained and re-inserted, plus O(k log k) canonical sort of the candidate set.
- `Snapshot`, `Clone`: O(n log n) canonical sort / O(n) copy respectively.
