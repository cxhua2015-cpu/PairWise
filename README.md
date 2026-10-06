# balanceledger292

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## Design notes

### Index

Account state lives in a single `map[string]Account` keyed by name, guarded by
one `sync.RWMutex`. Writers (`Apply`) take the exclusive lock; readers
(`Top`, `Snapshot`, `Stats`, `Clone`, `ValidateBatch`) take the read lock, so
all public methods are safe for concurrent use and readers never observe a
partially applied batch.

### Candidate transactions

`Apply` first runs the shared structural validation from `validation.go`
(kind, name charset/length, non-zero delta) without touching state. It then
stages each op against private per-name candidate entries copied out of the
map, checking int64 overflow before every addition, enforcing the absolute
value limit on every produced value, and checking the account-capacity limit
only against the final staged size. Only when every op succeeds are the
staged entries committed to the shared map, generation incremented once, and
the consecutive revisions (one per Add/Set) finalized — any failure discards
the candidate state, leaving no trace.

### Ownership

All values crossing the API boundary (`Result.Changed`, `Top`, `Snapshot`)
are freshly built slices of `Account` values, and `Clone` copies the map,
options and logical clocks into a brand-new ledger. No returned value shares
memory with internal state, so callers cannot mutate the ledger by aliasing.

### Complexity

- `Apply`: O(k) time for k ops, O(t) extra space for t touched accounts.
- `Top`: O(a log a) for a accounts (full sort, then truncate to n).
- `Snapshot`: O(a log a) (name sort); `Stats`: O(1); `Clone`: O(a).
- `ValidateBatch`: O(k) and side-effect free.
