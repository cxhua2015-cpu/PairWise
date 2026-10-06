# balanceledger252

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## Design notes

### Indexing

State lives in a single `map[string]Account` keyed by name, guarded by one
`sync.RWMutex`. `Top` and `Snapshot` materialize and sort on read
(value desc / name asc, and name asc respectively), so there is no secondary
index to keep consistent with the map — ordering invariants can never drift.

### Candidate transactions

`Apply` first runs the shared structural precheck (`ValidateBatch`, no state
access), then executes the batch against a private candidate copy of the map.
Revision allocation, overflow/absolute-limit checks, `ErrNotFound` detection,
and the final capacity check all happen on the candidate; only a fully
successful batch swaps the candidate in and bumps `generation` once. Any
failure simply discards the candidate, giving rollback for free.

### Ownership

`Account` is a plain value type, so map stores and returned slices never
share backing memory. `Snapshot`, `Top`, and `Result.Changed` hand out fresh
slices, and `Clone` copies the map and both logical clocks (`generation`,
`nextRevision`) into an independent ledger — mutations on either side never
alias the other.

### Complexity

- `Apply`: O(a + k) where a = live accounts (candidate copy) and k = ops.
- `ValidateBatch`: O(k · L), L = max name bytes; no state access.
- `Top` / `Snapshot`: O(a log a) sort per call.
- `Stats`: O(1). `Clone`: O(a).
- Space: O(a) plus O(a) transient per in-flight `Apply`/`Clone`.
