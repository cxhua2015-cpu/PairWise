# balanceledger227

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## Design notes

### Index

Account state lives in a single hash index (`map[string]Account`) guarded by
one `sync.RWMutex`. Writers (`Apply`) take the exclusive lock; readers
(`Top`, `Snapshot`, `Stats`, `Clone`, `ValidateBatch`) either take the read
lock or, like `ValidateBatch`, touch only immutable `Options` and need no
lock at all.

### Candidate transactions

`Apply` never mutates live state in place. It first runs the shared
structural precheck (`validateBatch`, no state access), then clones the
index into a private candidate map and replays the ops in input order
against it — allocating one consecutive revision per Add/Set, detecting
int64 overflow before any arithmetic, and enforcing the absolute-value
limit per result. The final account-capacity check runs only after the
whole batch has been applied to the candidate. On any error the candidate
is discarded (full rollback, clocks untouched); on success the map is
swapped in and `generation` advances exactly once.

### Ownership

All returned slices (`Result.Changed`, `Top`, `Snapshot.Accounts`) are
freshly allocated copies, and `Clone` deep-copies the index plus the
logical clocks (`generation`, `nextRevision`) under the read lock, so no
caller or clone can ever alias or mutate internal state.

### Complexity

- `Apply`: O(k·n) worst case to copy the index plus O(k) for k ops
  (n = number of accounts); validation alone is O(k).
- `Top`: O(n log n) sort, value descending then name ascending.
- `Snapshot`: O(n log n) sort by name; `Stats`: O(1); `Clone`: O(n).
