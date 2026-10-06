# balanceledger277

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## Design notes

### Indexing

Account state lives in a single `map[string]Account` owned by the `Ledger` and guarded by one `sync.RWMutex`. No secondary index is maintained: `Top` and `Snapshot` materialize and sort a fresh slice per call, so returned slices never alias internal state. `Apply` takes the write lock; `Top`, `Snapshot`, `Stats`, and `Clone` take the read lock, making every public method linearizable and safe for concurrent use.

### Candidate transactions

`Apply` first runs the shared structural precheck (`ValidateBatch`, no state access), then copies the account map into a candidate and replays ops in input order. Add/Set assign consecutive revisions from the local `nextRevision` cursor; int64 overflow is detected before each addition and the absolute-value cap is enforced per resulting balance, both yielding `ErrValue`. Final account capacity is checked only once, at batch end (`ErrCapacity`). Any failure discards the candidate, so the committed map, generation, and revision clock are untouched (full rollback). A non-empty successful batch bumps `generation` exactly once; an empty batch is a no-op.

### Ownership

`Clone` deep-copies the map plus both logical clocks (`generation`, `nextRevision`) under the read lock, producing a fully independent ledger — mutating the clone never affects the original. `Result.Changed`, `Top`, and `Snapshot` hand out value copies, so callers cannot reach mutable internal state.

### Complexity

- `Apply`: O(n·a) for n ops over a accounts (candidate copy O(a), each op O(1)).
- `ValidateBatch`: O(n) structural checks, no state reads.
- `Top`: O(a log a) sort, then truncated to k.
- `Snapshot`: O(a log a) name sort; `Stats`: O(1); `Clone`: O(a).
