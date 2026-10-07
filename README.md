# metacatalog436

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

- `servicecatalog.go` — core store, atomic batch `Apply`, `Get`, `Snapshot`.
- `validation.go` — `ValidateBatch`: pure structural pre-check (kind, name charset/length, value rules) shared by `Apply` and `Preview`; never reads or mutates state.
- `stats.go` — `Stats`: linearizable summary (generation, next revision, record count, total value bytes).
- `clone.go` — `Clone`: deep copy preserving the logical clocks (`Generation`, `NextRevision`) with fully independent ownership.
- `preview.go` — `Preview`: replays full transaction semantics on a private clone taken from one linearizable snapshot, returning the candidate `Result`, `Snapshot`, and `Stats` without touching the receiver's state, clocks, or ownership. Errors and their priority match `Apply` on the same state; failures return zero values.

## Design notes

- **Index**: records live in a `map[string]Record` keyed by name, giving O(1) average lookup for `Get` and per-op batch application. `Snapshot` sorts records by name on read.
- **Candidate transaction**: `Apply` validates structurally first, then applies the batch to a private working copy of the map under a single write lock. Record-count and total-value-byte capacity are checked only at batch end; any failure discards the working copy, so state, generation, and revision roll back atomically. Puts allocate consecutive revisions; deletes allocate none; a non-empty successful batch bumps `Generation` exactly once.
- **Ownership**: every byte slice crossing the API boundary (inputs on Put, outputs from `Get`/`Snapshot`/`Result`/`Clone`/`Preview`) is deep-copied, so callers and the store never share backing arrays.
- **Concurrency**: a single `sync.RWMutex` guards all state; writers serialize, readers (`Get`/`Snapshot`/`Stats`) run concurrently. `Clone` and `Preview` hold the read lock only long enough to snapshot, then work on private memory.
- **Complexity**: `Apply` is O(n + m) for n existing records and m ops (working-copy clone plus replay); `Get` O(1) average; `Snapshot` O(n log n) for sorting; `Stats` O(1); `Clone`/`Preview` O(n + m).
