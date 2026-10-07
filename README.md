# balanceledger432

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

- `creditpool.go` — public types, error values, and the core transaction engine (`New`, `Apply`, `Top`, `Snapshot`) plus the shared internal `state` model.
- `validation.go` — `ValidateBatch`: complete structural pre-check (kind, name charset/length, per-kind field discipline, pre-arithmetic absolute-value limit) that never reads or mutates state. `Apply` and `Preview` share exactly these structural semantics.
- `stats.go` — `Stats`: a linearizable summary (generation, next revision, account count) taken under the ledger lock.
- `clone.go` — `Clone`: a deep copy that preserves the logical clocks (generation and next revision) while sharing no memory with the original.
- `preview.go` — `Preview`: a candidate transaction executed on a private copy of the state captured under one lock hold; returns the candidate `Result`, `Snapshot` and `Stats` identical to a real `Apply` on that state, with identical error priority, and leaves the receiver's state and logical clocks untouched (zero values on failure).

## Indexing and concurrency

Accounts live in a `map[string]Account` owned exclusively by one `Ledger` and guarded by a single `sync.Mutex`; every public method takes the lock, so all operations are linearizable. `Top` and `Snapshot` materialize and sort fresh slices (`Top`: value descending, name ascending; `Snapshot`: name ascending), so returned slices never alias internal state.

## Candidate transactions

Both `Apply` and `Preview` run the batch against a private candidate copy of the state (`state.execute`). Only on full success — including the end-of-batch capacity check — does `Apply` commit the candidate back, which gives atomic rollback for free. `Preview` discards the candidate after reading out its result, snapshot and stats.

## Ownership

A `Ledger` never shares its `state` (map, clocks) with any other value: `Clone` deep-copies the map, `Snapshot`/`Top`/`Result.Changed` are freshly built slices, and `Preview` operates on a throwaway copy. Mutating any returned value or any clone cannot affect the original ledger.

## Complexity

Let `b` be the batch size and `n` the number of accounts.

- `Apply` / `Preview`: `O(b + n)` (candidate copy plus sequential op execution).
- `ValidateBatch`: `O(b · L)` where `L` is the name length bound; no state access.
- `Top`: `O(n log n)`; `Snapshot`: `O(n log n)`; `Stats`: `O(1)`; `Clone`: `O(n)`.
