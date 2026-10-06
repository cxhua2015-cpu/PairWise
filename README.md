# metacatalog231

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## Architecture notes

### Index
`Store` keeps a single primary index `map[string]Record` keyed by name, guarded by a `sync.RWMutex`. Reads (`Get`, `Snapshot`, `Stats`, `Clone`) take the read lock; `Apply` takes the write lock, so all public methods are linearizable and safe for concurrent use. `Snapshot` materializes records sorted by name.

### Candidate transaction
`Apply` first runs the shared structural validation (`ValidateBatch`) without touching state, then applies ops in input order to a candidate map copied from the index, allocating revisions on a candidate counter (Put allocates, Delete does not). Record-count and total-value-bytes capacity are checked only against the final candidate. On any error the candidate is discarded, so state, generation and revision roll back together; on success the candidate is swapped in and a non-empty batch bumps generation exactly once.

### Ownership
All Values are deep-copied on the way in (`Apply`) and on the way out (`Get`, `Snapshot`, `Result.Changed`, `Clone`), so callers can never alias or mutate internal state. `Clone` copies the logical clocks (generation, next revision) and every record, yielding a fully independent store.

### Complexity
- `Apply`: O(n + m) for n ops and m existing records (candidate copy), plus O(k log k) to sort k changed names.
- `Get`: O(1) average. `Snapshot`/`Clone`: O(m log m) / O(m). `Stats`: O(m). `ValidateBatch`: O(n), no state access.
