# metacatalog251

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

## Design notes

### Index

The store keeps a single in-memory primary index: `map[string]Record`, guarded by a
`sync.RWMutex`. All lookups (`Get`) are O(1); writers (`Apply`) take the exclusive
lock, readers (`Get`, `Snapshot`, `Stats`, `Clone`) share the read lock. `Snapshot`
and `Result.Changed` are materialized in sorted-name order (O(n log n) sort over the
live key set). A running `totalValue` counter tracks aggregate value bytes in O(1)
per mutation, so end-of-batch capacity checks never rescan the index.

### Candidate transaction

`Apply` runs in two phases. Phase one is pure structural validation shared with
`ValidateBatch` (kind, name charset/length, value length, Delete-carries-no-value)
and touches no state. Phase two executes ops in input order against the live map
while recording a per-name backup of the pre-image on first touch. Puts allocate
consecutive revisions from a local counter; Deletes allocate none. Record-count and
total-value capacity limits are checked only once, at batch end. Any failure
(`ErrNotFound`, `ErrCapacity`) restores the saved pre-images and the byte counter,
leaving records, generation, and revision exactly as before the batch — a
candidate-transaction rollback without a shadow copy of the whole store.

### Ownership

All `Value` byte slices are copied on the way in (`Apply`) and on the way out
(`Get`, `Snapshot`, `Result.Changed`, `Clone`), so callers can never mutate
internal state and the store never retains caller buffers. `Clone` additionally
copies the logical clocks (`generation`, `nextRevision`) and the byte counter,
producing a fully independent store.

### Complexity

- `Apply`: O(k + c log c) for k ops and c distinct changed names (final sort), plus O(1) capacity checks.
- `Get`: O(1) average, plus O(v) copy of the value.
- `Snapshot` / `Clone`: O(n log n) / O(n) over n records, with O(V) total value-copy bytes.
- `Stats`: O(1), linearizable under the read lock.
- `ValidateBatch`: O(k) with no state access and no side effects.
