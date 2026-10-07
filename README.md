# balanceledger417

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## Design notes

### Indexing

The ledger keeps a single `map[string]Account` as its primary index, guarded by a
`sync.RWMutex`. `Apply` holds the write lock; `Top`, `Snapshot`, `Stats`, and
`Clone` take the read lock. `Top` and `Snapshot` materialize and sort on demand —
`Top` by (value desc, name asc), `Snapshot` by name — so there is no secondary
index to keep consistent, and every reader observes a linearizable state.

### Candidate transactions

`Apply` first runs the same structural preflight as `ValidateBatch`
(`validation.go`), then executes the batch against a private candidate map copied
from the live index. All overflow/absolute-value checks happen per op before any
mutation is committed, and the account-capacity check runs once at batch end
against the candidate. Only on success are the candidate map and the logical
clocks (generation, next revision) swapped in, so failures roll back atomically
with zero partial writes.

### Ownership

All returned slices (`Result.Changed`, `Top`, `Snapshot.Accounts`) are freshly
allocated and never alias internal state. `Clone` deep-copies the account map and
carries over the logical clocks, producing a fully independent ledger.

### Complexity

- `Apply`: O(k·n) worst case for the candidate copy (n accounts, k ops); O(k) work per op.
- `ValidateBatch`: O(k), no state access.
- `Top`: O(n log n); `Snapshot`: O(n log n); `Stats`: O(1); `Clone`: O(n).
