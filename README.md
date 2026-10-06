# readyqueue235

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

## Design notes

### Index

Items live in a single `map[string]Item` keyed by ID, guarded by one `sync.Mutex`. The map gives O(1) existence checks for `Enqueue`/`Cancel`; canonical pop order (Priority desc, ReadyAt asc, ID asc) is produced by sorting the ready candidates on read. A single mutex keeps every public method linearizable without lock ordering hazards.

### Candidate transaction

`Apply` first runs the shared side-effect-free structural precheck (`validateBatchStruct` in `validation.go`), then takes the lock, enforces the monotone clock, and executes ops sequentially against the live map while recording an undo log. Capacity (`MaxItems`) is checked only once at the very end. Any failure (`ErrExists`, `ErrNotFound`, `ErrCapacity`) replays the undo log in reverse and restores `now` and `nextRev`, so time, items, revisions and generation are rolled back atomically. Empty batches advance the clock but never bump `generation`; a non-empty successful batch bumps it exactly once.

### Ownership

`Snapshot` and `Pop` return freshly allocated slices, and `Clone` (in `clone.go`) builds a brand-new map plus copied logical clocks (`now`, `generation`, `nextRev`). No returned value aliases internal state, so callers may mutate results freely and clones evolve fully independently of the original.

### Complexity

- `New`, `ValidateBatch`: O(batch size) for validation; `New` itself is O(1).
- `Apply`: O(k) for k ops, plus O(k) undo work only on failure; final capacity check is O(1).
- `Pop`: O(n + r log r) for n stored items and r ready candidates (filter + sort), deletion included.
- `Snapshot`, `Clone`: O(n log n) / O(n) respectively; `Stats` is O(1).
