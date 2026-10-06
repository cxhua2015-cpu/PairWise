# balanceledger292

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

## Design notes

- **Index**: state lives in a single `map[string]Account` guarded by a `sync.RWMutex`. Reads (`Top`, `Snapshot`, `Stats`, `Clone`) take the read lock; `Apply` takes the write lock for its whole critical section, so every batch is atomic and every read is linearizable.
- **Candidate transaction**: `Apply` first runs `ValidateBatch` (pure structural check, no state access), then applies all ops to a private copy of the account map. Arithmetic overflow is detected before each addition, and the absolute-value limit is enforced per result; the account-capacity limit is checked only against the final candidate map. The candidate replaces live state only when every check passes, so failures roll back with zero side effects and the generation counter advances exactly once per non-empty committed batch.
- **Ownership**: all returned slices (`Result.Changed`, `Top`, `Snapshot.Accounts`) are freshly allocated, and `Clone` deep-copies the map plus the logical clocks (`generation`, `nextRevision`), so callers and clones can never alias internal state.
- **Complexity**: `Apply` is O(k + n) for k ops over n accounts (map copy), `ValidateBatch` is O(k), `Top`/`Snapshot` are O(n log n), `Stats` is O(1), and `Clone` is O(n).
