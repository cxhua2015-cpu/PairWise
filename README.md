# balanceledger242

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## Design notes

### Indexing

Account state lives in a single `map[string]Account` guarded by one
`sync.RWMutex`. No secondary indexes are maintained: `Top` sorts a
materialized copy by (value desc, name asc) and `Snapshot` sorts by name
on every call, which keeps the write path O(1) per op and the read path
O(n log n) with n = number of live accounts.

### Candidate transactions

`Apply` first runs the shared structural precheck (`ValidateBatch`,
side-effect free), then executes the batch against a private candidate
map cloned from current state. Overflow and absolute-value checks run
before each arithmetic step; the final account-capacity check runs once
at batch end. Any failure simply discards the candidate, so failed
batches leave zero observable trace. On success the candidate is swapped
in, `generation` increments exactly once (empty batches leave it
unchanged), and `nextRevision` advances by the number of Add/Set ops.

### Ownership

All returned slices (`Result.Changed`, `Top`, `Snapshot.Accounts`) are
freshly allocated copies; callers can mutate them freely. `Clone`
deep-copies the account map plus the logical clocks (`generation`,
`nextRevision`) under the read lock, so the clone is fully independent
of — and linearizable with — the original.

### Complexity

- `Apply`: O(k + n) time, O(k + n) space for k ops and n accounts.
- `ValidateBatch`: O(k), no state access.
- `Top` / `Snapshot`: O(n log n).
- `Stats`: O(1). `Clone`: O(n).
