# balanceledger297

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

## Design notes

- **Index**: account state lives in a single `map[string]Account` guarded by a `sync.RWMutex`. `Top` and `Snapshot` materialize and sort a copy on each call (no secondary index), so writers never maintain auxiliary structures. `Top` sorts by value descending then name ascending; `Snapshot` sorts by name.
- **Candidate transaction**: `Apply` first runs the shared structural precheck (`ValidateBatch`, no state access), then executes the batch against a private copy of the account map. int64 overflow is detected before any arithmetic, the absolute-value limit is enforced per resulting balance, and the account-capacity limit is checked only once at the end of the batch. Any failure discards the candidate map, so the committed state, generation, and revision clock are untouched (full rollback). On success the candidate map is swapped in and a non-empty batch bumps `generation` exactly once; `Add`/`Set` allocate consecutive revisions from `nextRevision`.
- **Ownership**: all returned slices (`Result.Changed`, `Top`, `Snapshot.Accounts`) are freshly allocated copies, and `Clone` deep-copies the map along with the logical clocks (`generation`, `nextRevision`). Mutating a returned value or applying batches to a clone never affects the source ledger.
- **Complexity**: with `n` accounts and `b` ops per batch — `Apply` O(n + b) time / O(n + b) space (map copy plus changed list), `ValidateBatch` O(b), `Top` O(n log n), `Snapshot` O(n log n), `Stats` O(1), `Clone` O(n). Read paths use the read lock, so concurrent `Top`/`Snapshot`/`Stats`/`Clone` calls proceed in parallel while `Apply` serializes.

## Files

- `creditpool.go` — core ledger: options, atomic batch engine, `Top`, `Snapshot`.
- `validation.go` — side-effect-free structural batch precheck shared with `Apply`.
- `stats.go` — linearizable state summary.
- `clone.go` — ownership-safe deep copy preserving logical clocks.
