# balanceledger282

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## Design notes

- **Index**: accounts live in a `map[string]Account` guarded by a single `sync.RWMutex`; writers take the exclusive lock, readers (`Top`, `Snapshot`, `Stats`, `Clone`) share the read lock.
- **Candidate transaction**: `Apply` first runs `ValidateBatch` (pure structural check, no state access), then replays ops in input order on a private candidate map. Overflow is checked before arithmetic, the absolute-value cap is enforced per result, and the account-capacity cap is checked only at batch end. Any failure discards the candidate, leaving committed state untouched (full rollback).
- **Ownership**: `Account` is a value type, so the candidate map, `Changed`, `Top`, `Snapshot`, and `Clone` results never alias internal state; `Clone` copies the map plus the logical clocks (`generation`, `nextRevision`) and is fully independent.
- **Complexity**: `Apply` is O(n) in batch size plus O(a) map copy; `Top` and `Snapshot` are O(a log a) for sorting (value desc / name asc, and name asc respectively); `Stats` and `ValidateBatch` are O(1) / O(n).
