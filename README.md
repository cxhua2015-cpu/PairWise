# expirytable404

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

- `heartbeat.go` — core transaction engine: `Table`, `New`, `Apply`, `Expire`, `Snapshot`.
- `validation.go` — side-effect-free structural preflight shared by `Apply` and `ValidateBatch`.
- `stats.go` — linearizable `Stats` summary.
- `clone.go` — ownership-safe deep copy preserving logical clocks.

## Design notes

**Index.** Entries live in a single `map[string]Entry` guarded by one `sync.Mutex`, giving O(1) average lookup/insert/delete by key. `Snapshot` and `Expire` return entries sorted by key (canonical order); every returned slice is freshly allocated and isolated from internal state.

**Candidate transaction.** `Apply` first runs the shared structural validation (no state access), then checks monotonic time under the lock. It builds a candidate map: entries with `ExpiresAt <= Now` are evicted (closed interval), then `Put`/`Touch`/`Delete` replay in order, with `Put`/`Touch` allocating revisions. Only if the final size fits `MaxEntries` is the candidate swapped in; any error (`ErrTime`, `ErrNotFound`, `ErrCapacity`) discards the candidate and rolls back evictions, time, revisions, and generation together. A non-empty successful batch bumps `generation` exactly once; an empty batch changes nothing.

**Ownership.** `Clone` copies the map, options, and logical clocks (`now`, `generation`, `nextRev`) under the lock, so the clone is fully independent — later transactions on either table never alias the other.

**Complexity.** `Apply` is O(n + m) for n live entries and m ops; `Expire` and `Snapshot` are O(n log n) due to canonical sorting; `Stats` is O(1); `Clone` is O(n); `ValidateBatch` is O(m) and touches no state.
