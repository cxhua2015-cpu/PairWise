# balanceledger257

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## Design notes

### Indexing
Account state lives in a single `map[string]Account` keyed by name, guarded by one `sync.RWMutex`. There is no secondary index: `Top` sorts a materialized slice by (value desc, name asc) and `Snapshot` sorts by name on demand. This keeps the write path O(1) per op and pushes ordering cost to readers.

### Candidate transactions
`Apply` runs in two phases under the write lock. First `ValidateBatch` performs pure structural checks (kind, name charset/length, non-zero delta, absolute-value caps on inputs) without touching state. Then the batch is replayed against a copied candidate map: `Add` checks int64 overflow *before* arithmetic and enforces `MaxAbsValue` on every intermediate result, `Set` assigns directly, and `Delete` requires existence. Revisions are allocated contiguously to `Add`/`Set` ops only. The final account-count capacity check happens once, at the end of the batch. Any failure discards the candidate map — the committed state, generation, and revision clock are untouched, giving full rollback. A successful non-empty batch bumps `generation` exactly once; empty batches change nothing.

### Ownership
All returned slices (`Result.Changed`, `Top`, `Snapshot.Accounts`) are freshly allocated copies; callers cannot mutate internal state through them. `Clone` deep-copies the map plus the logical clocks (`generation`, `nextRevision`) under the read lock, so the clone is fully independent — later writes to either ledger never alias the other.

### Complexity
- `Apply`: O(k·n) worst case for a batch of k ops over n accounts (candidate map copy dominates), O(k) extra space.
- `ValidateBatch`: O(k), no state access.
- `Top(m)`: O(n log n) sort, O(n) space.
- `Snapshot`: O(n log n) sort by name, O(n) space.
- `Stats`: O(1) under read lock.
- `Clone`: O(n) time and space.
