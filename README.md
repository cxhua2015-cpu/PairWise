# barrier

Concurrent-safe, in-memory, multi-generation barrier registry. See `SPEC.md` for the
normative contract; this file describes the implementation.

## Design

### Indexing
The registry keeps barriers in a `map[string]*barrierState` keyed by barrier name,
so lookup, arrival, and cancellation are O(1) per operation. Each barrier tracks its
fixed `Parties`, its current `Generation` (starting at 1), and its pending arrivals
as a `map[string]struct{}` for O(1) duplicate/membership checks. A single
`sync.Mutex` guards the whole registry, making every `Apply` and `Snapshot` atomic.

### Candidate transaction
`Apply` runs in two phases under the contract's validation order:

1. **Structural validation** of every op (kind, name/participant shape) without
   touching state. Any failure returns `ErrInvalidInput` before semantics run.
2. **Transactional execution**: the registry clones all barrier state (generation
   plus pending sets) into per-barrier *candidate* structures, then applies ops in
   input order against the candidates. Any semantic error (`ErrNotFound`,
   `ErrConflict`, or the final `ErrCapacity` check) simply discards the candidates —
   nothing was mutated, so rollback is free and no completion or generation can
   leak. On success the candidates are committed wholesale and the registry
   generation increments exactly once (nonempty batches only).

### Generation advancement
When a barrier's pending set reaches exactly `Parties`, it completes immediately:
participants are sorted, a `Completion` carrying the barrier's current generation is
appended in operation-occurrence order, the pending set is cleared, and the barrier
generation increments. Later ops in the same batch therefore arrive into the next
generation, so the same participant may appear again after a completion boundary.

### Capacity accounting
`MaxPending` bounds the *final* total of pending arrivals across all barriers,
evaluated only after the whole batch executes. Completions reduce the running total
mid-batch, so a batch may transiently hold more than `MaxPending` as long as it
finishes at or below the limit. Exceeding it returns `ErrCapacity` and rolls back
the entire batch, including any completions already produced.

### Complexity
For a batch of `n` ops over `B` barriers with `P` total pending arrivals:

- Structural validation: O(total input bytes).
- Candidate clone: O(P) to copy pending sets, O(B) for generations.
- Execution: O(n) map operations, plus O(P log P) sorting per completion.
- Commit: O(B).
- `Snapshot`: O(B log B) to sort barrier names plus O(P log P) to sort pending lists.

Memory is O(B + P). All returned slices are freshly allocated and sorted, so
results and snapshots are ownership-isolated from the registry and from each other.

## Verification

- `go test ./...` — contract tests plus supplementary tests (cross-generation
  batches, cancel, duplicate conflicts, capacity, isolation, rollback, ordering,
  ownership, concurrency).
- `go test -race ./...` — race-detector clean.
- `go run ./cmd/demo` — prints `registryGeneration=1 completions=1 barrierGeneration=2 pending=0`.
