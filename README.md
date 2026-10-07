# readyqueue425

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

- `prioritybox.go` — core types plus the `Apply`/`Pop`/`Snapshot` transaction engine.
- `validation.go` — side-effect-free structural batch validation shared by `Apply` and `ValidateBatch`.
- `stats.go` — linearizable `Stats` summary.
- `clone.go` — ownership-safe deep copy preserving logical clocks.
- `preview.go` — `Preview`, a non-mutating transactional dry run.

## Indexing and ordering

Items live in a `map[string]Item` keyed by ID for O(1) existence checks during `Enqueue`/`Cancel`. Canonical order (Priority desc, ReadyAt asc, ID asc) is computed on read: `Pop` and `Snapshot` collect and sort the relevant items, so a batch of k ops costs O(k) map updates plus O(n log n) sorting on reads over n items. `Pop` filters by `ReadyAt <= now` before sorting and truncating to the requested count.

## Candidate transactions and rollback

`Apply` validates the whole batch structurally first, then executes ops against a private candidate map cloned from the live state. Revisions are handed out from a candidate counter. Capacity is checked only once, at the end, against the candidate size. The candidate state, revision counter, logical clock and a single generation bump are committed only when every op succeeds; any failure (`ErrExists`, `ErrNotFound`, `ErrCapacity`, `ErrTime`) discards the candidate, leaving time, state and revision untouched. Empty successful batches change nothing.

## Preview

`Preview` takes one linearizable `Clone` of the queue, runs the exact same `Apply` semantics on that candidate, and returns the candidate `Result`, `Snapshot` and `Stats`. The receiver's state, generation, revision and logical clock are never modified, and failures return the same error `Apply` would produce on the same state, with all result values zero.

## Ownership and concurrency

A single mutex linearizes all public methods. `Snapshot` and `Pop` return freshly allocated slices, `Clone` copies the item map, and `Preview` works on its own candidate, so no returned value ever aliases internal state and cloned queues are fully independent of their source.
