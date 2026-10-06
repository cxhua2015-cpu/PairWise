# balanceledger262

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## Design notes

- **Index**: accounts live in a single `map[string]Account` keyed by name. `Top` and `Snapshot` materialize and sort a copy on demand (`Top`: value desc, name asc; `Snapshot`: name asc), so no secondary index is maintained.
- **Candidate transaction**: `Apply` first runs the shared structural validation (`ValidateBatch`, side-effect free), then clones the account map into a candidate and replays ops in input order. Add/Set allocate consecutive revisions; int64 overflow is detected before arithmetic and the absolute-value limit is enforced per result. Final account capacity is checked only at batch end. Any failure discards the candidate — the committed state is untouched (full rollback). On success the candidate is swapped in and `generation` increments exactly once (empty batches leave it unchanged).
- **Ownership**: all returned slices (`Result.Changed`, `Top`, `Snapshot.Accounts`) are freshly built copies; `Clone` deep-copies the map and logical clocks (`generation`, `nextRevision`) so the clone is fully independent.
- **Concurrency**: a single `sync.RWMutex` guards all state; writers take the exclusive lock, readers (`Top`, `Snapshot`, `Stats`, `Clone`) share the read lock, making every public method linearizable.
- **Complexity**: `Apply` is O(k + n) for k ops over n accounts (candidate copy); `Top`/`Snapshot` are O(n log n); `Stats` is O(1); `Clone` is O(n). Memory is O(n) plus the O(n) candidate during a batch.
