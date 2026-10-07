# metacatalog406

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## Design notes

### Index

Records live in a single `map[string]Record` keyed by name, guarded by one
`sync.RWMutex`. The map is the only index: name lookup, insert and delete are
O(1) average. A running `total` counter tracks the sum of value byte lengths
so capacity checks are O(1) instead of rescanning the map.

### Candidate transaction

`Apply` is a two-phase candidate transaction. Phase one (`ValidateBatch` in
`validation.go`) performs complete structural validation — kind, name
alphabet/length, value size, and Delete-carries-no-value — without touching
state. Phase two takes the write lock and applies ops in input order: each
`Put` deep-copies its value and allocates the next revision; `Delete` removes
without allocating. Before mutating a name for the first time, its prior
entry is pushed onto an undo log. Record-count and total-value-byte limits
are checked only at batch end. Any failure (`ErrNotFound`, `ErrCapacity`)
replays the undo log and restores `nextRev`, `total` and `generation`, so a
failed batch leaves zero observable trace. A non-empty successful batch
increments `generation` exactly once; an empty batch changes nothing.

### Ownership

The store never aliases caller memory and callers never alias store memory:
`Put` values are copied on the way in, and `Get`, `Snapshot`, `Result.Changed`
and `Clone` all return freshly allocated value slices. Mutating a returned
slice or the input after `Apply` cannot corrupt the catalog.

### Complexity

- `Apply`: O(k) for k ops, plus O(m log m) to sort the m touched names in
  `Result.Changed`.
- `Get` / `Stats`: O(1) (value copy aside).
- `ValidateBatch`: O(k), no state access.
- `Snapshot` / `Clone`: O(n) in the number of records, with Snapshot sorting
  names in O(n log n). Both hold the read lock, so they are linearizable
  against concurrent transactions.
