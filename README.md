# metacatalog281

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

- `servicecatalog.go` — store state, atomic batch transaction engine, `Get`/`Snapshot`.
- `validation.go` — pure structural preflight shared by `ValidateBatch` and `Apply`.
- `stats.go` — linearizable `Stats` summary read under the same lock as transactions.
- `clone.go` — deep copy preserving logical clocks with fully independent ownership.

## Index

Records live in a single `map[string]Record` keyed by name, guarded by a
`sync.RWMutex`. Writers (`Apply`) take the exclusive lock; readers (`Get`,
`Snapshot`, `Stats`, `Clone`) take the read lock, so all public methods are
concurrency-safe and every observation is linearizable. Sorted output
(`Snapshot`, `Result.Changed`) is produced by sorting the map keys on demand.

## Candidate transaction

`Apply` first runs the shared structural validation (no state reads), then
executes ops in input order against the live map while recording a per-name
backup of the previous entry (or its absence). Puts allocate consecutive
revisions; deletes allocate none. Record-count and total-value-byte capacity
are checked only at the end of the batch. Any failure (`ErrNotFound`,
`ErrCapacity`) restores the backed-up entries, leaving records, generation and
revision exactly as before the batch. A non-empty successful batch bumps
`generation` exactly once; an empty batch changes nothing.

## Ownership

All `[]byte` values crossing the API boundary are copied: inputs are cloned on
Put, and `Get`/`Snapshot`/`Result.Changed` return fresh slices, so callers can
never mutate internal state (or vice versa). `Clone` copies every record's
value into a new store, giving the clone fully independent ownership while
preserving `generation` and `nextRevision`.

## Complexity

- `Apply`: O(k) ops plus O(m log m) to sort the m changed names; rollback is O(k).
- `Get` / `Stats`: O(1) (plus O(v) copy of the value for `Get`).
- `Snapshot`: O(n log n) for n records, with O(V) bytes copied (V = total value bytes).
- `Clone`: O(n) records, O(V) bytes copied.
- `ValidateBatch`: O(k), no state access, no allocation beyond the batch itself.
