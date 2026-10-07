# metacatalog411

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

- `servicecatalog.go` — store state, atomic batch transaction engine, `Get`, `Snapshot`.
- `validation.go` — pure structural preflight (`ValidateBatch`), shared by `Apply`.
- `stats.go` — linearizable aggregate statistics (`Stats`).
- `clone.go` — deep copy preserving logical clocks (`Clone`) plus the shared byte-copy helper.

## Index

Records live in a single `map[string]Record` keyed by name, guarded by one
`sync.RWMutex`. Writers (`Apply`) take the write lock; readers (`Get`,
`Snapshot`, `Stats`, `Clone`) take the read lock, so all public methods are
safe for concurrent use and every observation is linearizable. `Snapshot`
sorts records by name on each call.

## Candidate transaction

`Apply` first runs the shared structural validation (no state reads), then
executes ops in input order against an in-memory change set: each `Put`
assigns the next consecutive revision, `Delete` assigns none and fails with
`ErrNotFound` when the name exists neither in committed state nor earlier in
the batch. The change set is then folded into a candidate map, and only at
batch end are `MaxRecords` and `MaxTotalValueBytes` checked. On any failure
the committed map, `generation`, and `nextRevision` are untouched (rollback
by construction, since the candidate is discarded); on success the candidate
is swapped in and a non-empty batch bumps `generation` exactly once.

## Ownership

All `Value` byte slices are copied on the way in (`Put`) and on the way out
(`Get`, `Snapshot`, `Result.Changed`, `Clone`), so callers can never mutate
or observe internal state. `Clone` copies the logical clocks (`generation`,
`nextRevision`) and every record into fresh slices, yielding a fully
independent store.

## Complexity

Let `n` be the number of stored records, `b` the batch size, and `v` the
total value bytes touched.

- `Apply`: `O(n + b)` time to build the candidate map, `O(b log b)` to sort
  the changed-name list, `O(v)` copying.
- `Get` / `Stats`: `O(1)` (plus `O(len(value))` copy for `Get`).
- `Snapshot` / `Clone`: `O(n)` copying; `Snapshot` adds `O(n log n)` sorting.
- `ValidateBatch`: `O(b)` structural checks, no state access.

Space is `O(n)` for the store plus `O(n)` transient for the candidate map
during `Apply`.
