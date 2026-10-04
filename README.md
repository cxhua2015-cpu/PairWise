# barrier

See `SPEC.md`. Implement the `barrier` package without changing the public contract.

## Implementation notes

- **Index**: the registry keeps barriers in a `map[string]*barrierState` for O(1) lookup by name, plus a sorted `[]string` of names so `Snapshot` emits barriers in name order without re-sorting.
- **Candidate transaction**: `Apply` first validates every op structurally (kind, name/participant shape) without touching state, then clones all barrier states into a candidate map. Ops execute in input order against the candidate; any error (`ErrNotFound`, `ErrConflict`, `ErrCapacity`) simply discards it, so no completions, generations, or pending arrivals leak. On success the candidate map replaces the live map and a nonempty batch bumps the registry generation exactly once.
- **Generation advance**: each barrier tracks its own generation (starting at 1). When pending arrivals reach `Parties`, participants are sorted, a `Completion` is emitted with the current generation, the pending set is cleared, and the barrier generation increments — so later ops in the same batch enter the next generation.
- **Capacity accounting**: a running total of pending arrivals is maintained during the batch (arrivals +1, cancels −1, completions −Parties). The `MaxPending` check runs only once, after the last op, so intermediate completions can keep an oversized batch legal.
- **Complexity**: validation is O(total name bytes). Execution is O(1) average per arrive/cancel (map ops) plus O(P log P) per completion for sorting P participants. Cloning costs O(total pending) per batch. `Snapshot` is O(B + P log P) over B barriers and P pending participants. A single `sync.Mutex` serializes all methods, making the registry concurrency-safe.
