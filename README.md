# metacatalog251

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## Design notes

### Index

`Store` keeps a single in-memory index: `map[string]Record` keyed by name, guarded by one `sync.RWMutex`. Alongside it live the logical clocks (`generation`, `nextRevision`) and a running `totalValueBytes` counter, so statistics are O(1) and never require a scan. All public methods take the lock (read or write), making every method safe for concurrent use.

### Candidate transaction

`Apply` is a two-phase transaction:

1. **Structural preflight** — `ValidateBatch` checks every op (kind, name charset/length, value length, nil Delete payload) without touching state. `Apply` and `ValidateBatch` share this exact code path, so they can never disagree on structural validity.
2. **Candidate execution** — the index is copied into a private candidate map and the ops are replayed in input order. `Put` allocates consecutive revisions from a local `nextRevision` cursor; `Delete` allocates none and fails with `ErrNotFound` if the name is absent. Record-count and total-value-byte capacities are checked only once, at batch end, against the candidate state.

Only if everything succeeds does the store swap in the candidate index, bump `generation` (exactly once per non-empty batch) and advance `nextRevision`. Any failure discards the candidate, so state, generation and revision roll back completely. `Result.Changed` holds the final record per touched name (deletes omitted), sorted by name, with deep-copied values.

### Ownership

The store never aliases caller memory. `Put` copies the input value before storing; `Get`, `Snapshot` and `Result.Changed` return freshly allocated value slices; `Clone` deep-copies every record while preserving `generation` and `nextRevision`. Mutating any returned slice or the original input after the call cannot affect stored state, and a clone is fully independent of its source.

### Complexity

- `Apply`: O(n + m) time, O(m) extra space — n ops, m existing records (candidate copy).
- `Get`: O(1). `Stats`: O(1). `ValidateBatch`: O(n).
- `Snapshot`: O(m log m) for the name sort, O(total bytes) for copies.
- `Clone`: O(m) records plus O(total bytes) for value copies.
