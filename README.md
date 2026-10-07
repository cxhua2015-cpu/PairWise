# balanceledger402

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## Design notes

### Indexing

State lives in a single `map[string]account` guarded by a `sync.RWMutex`; there is no secondary index. `Top` and `Snapshot` copy the map into a fresh slice and sort on demand (`Top`: value descending, name ascending; `Snapshot`: name ascending). This keeps writes O(1) and avoids index-invalidation bugs; reads are O(n log n) in the number of accounts, which is bounded by `Options.MaxAccounts`.

### Candidate transactions

`Apply` is a two-phase candidate transaction. Phase one (`ValidateBatch`, shared with the public preflight in `validation.go`) performs complete structural validation without reading state. Phase two stages every op, in input order, on a private copy of the account map — the "candidate". Overflow is detected *before* each addition via `MaxInt64 - delta` / `MinInt64 - delta` comparisons, and the absolute-value cap is enforced on every intermediate value; the account-capacity cap is checked only once, on the final candidate. Any failure simply discards the candidate, so rollback is free and the committed state is never partially mutated. On commit the candidate is swapped in and a non-empty batch advances `generation` exactly once; each Add/Set consumes one contiguous revision.

### Ownership

Returned slices (`Result.Changed`, `Top`, `Snapshot.Accounts`) are freshly allocated copies — callers can mutate them without touching ledger state. `Clone` deep-copies the map plus the logical clocks (`generation`, `nextRevision`) under the read lock, so the clone is fully independent and linearizable with respect to concurrent batches.

### Complexity

- `Apply`: O(k·n) worst case for a batch of k ops over n accounts (candidate copy O(n) + k map ops).
- `Top` / `Snapshot`: O(n log n).
- `Stats`: O(1). `ValidateBatch`: O(k·L) for name length L. `Clone`: O(n).
- Locking: writers take the mutex exclusively; readers (`Top`, `Snapshot`, `Stats`, `Clone`) share the read lock.
