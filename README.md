# metacatalog291

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## Design notes

### Indexing

The store keeps a single in-memory index: `map[string]Record` keyed by name,
guarded by a `sync.RWMutex`. Writers (`Apply`) take the exclusive lock;
readers (`Get`, `Snapshot`, `Stats`, `Clone`) take the read lock, so reads
proceed concurrently with each other and are linearizable against commits.
`Snapshot` materializes a name-sorted, deep-copied slice on each call, so no
sorted structure is maintained on the write path.

### Candidate transaction

`Apply` runs in two phases. First `ValidateBatch` performs complete
structural validation (kind, name charset/length, value length, Delete
payload) without touching state — `Apply` and `ValidateBatch` share exactly
this code path. Then, under the write lock, ops are replayed in input order
onto a candidate map copied from the live index: each `Put` allocates the
next consecutive revision and deep-copies its value; `Delete` removes the
entry or fails with `ErrNotFound`. Record-count and total-value-bytes
capacity are checked only against the final candidate. Any failure discards
the candidate, leaving records, generation, and the revision clock untouched;
on success the candidate is swapped in and generation advances exactly once
(empty batches leave it unchanged).

### Ownership

All `Value` bytes are copied on the way in (`Put`) and on the way out
(`Get`, `Snapshot`, `Result.Changed`, `Clone`). Mutating a caller's slice
after `Apply`, or mutating a returned record, never affects stored state.
`Clone` additionally copies the logical clocks (`generation`,
`nextRevision`) and options, producing a fully independent store.

### Complexity

- `Apply`: O(n + m) time, O(m) extra space for n ops and m live records
  (candidate copy), plus O(k log k) to sort the k changed records.
- `Get`: O(1) average. `Stats`: O(m). `Snapshot`/`Clone`: O(m log m) / O(m).
- `ValidateBatch`: O(n), no state access.
