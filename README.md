# readyqueue440

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

- `readyqueue440/prioritybox.go` — core types plus the `Apply`/`Pop`/`Snapshot` transaction engine.
- `readyqueue440/validation.go` — `ValidateBatch`, the side-effect-free structural precheck shared with `Apply`.
- `readyqueue440/stats.go` — `Stats`, a linearizable state summary.
- `readyqueue440/clone.go` — `Clone`, a deep copy preserving the logical clock.
- `readyqueue440/preview.go` — `Preview`, a candidate transaction replayed on an isolated snapshot.

## Index and ordering

Items live in a `map[string]Item` keyed by ID, giving O(1) existence checks for `Enqueue`/`Cancel` and O(1) deletes in `Pop`. There is no maintained heap: `Pop` and `Snapshot` scan the map and sort the matching items by the canonical order — `Priority` descending, `ReadyAt` ascending, `ID` ascending. Revisions are assigned from a monotonic `nextRevision` counter (starting at 1) at `Enqueue` time and are never reused, even after `Cancel` or a rolled-back batch.

## Transactions and candidates

`Apply` first runs the full structural validation (`ValidateBatch`) without touching state, then — under a single mutex — checks the monotonic clock, replays the ops against a private copy of the item map, and only commits (map, `now`, `nextRevision`, and a single `generation` bump for non-empty batches) after the final capacity check passes. Any failure (`ErrTime`, `ErrExists`, `ErrNotFound`, `ErrCapacity`) simply discards the copy, so time, state and revision roll back for free.

`Preview` builds a candidate transaction: it takes one linearizable snapshot via `Clone` (under the same lock), runs the unmodified `Apply` on that candidate, and returns the candidate `Result`, `Snapshot` and `Stats` — exactly what a real `Apply` on the same state would have produced. The receiver's state, generation, revision counter and logical clock are untouched; failures return the same error as `Apply` with all other values zero.

## Ownership

All returned slices (`Pop`, `Snapshot`, and the `Snapshot` inside `Preview`) are freshly allocated and sorted copies; mutating them never affects the queue. `Clone` copies the item map, so the original and the clone (and a `Preview` candidate) share no mutable state. Every public method takes the queue mutex, so all methods are safe for concurrent use and each observes a linearizable state.

## Complexity

- `New`, `Stats`: O(1).
- `ValidateBatch`: O(ops · idLen), no state access.
- `Apply`: O(items + ops) to copy and replay, plus the final O(1) capacity check.
- `Pop`: O(items + k log k) for k ready items (scan + sort of up to `limit` results).
- `Snapshot`: O(items log items) for the sorted copy.
- `Clone`: O(items).
- `Preview`: O(items + ops) for the clone plus the candidate `Apply`, plus O(items log items) for the candidate snapshot.
