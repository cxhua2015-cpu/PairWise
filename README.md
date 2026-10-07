# readyqueue430

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## Design notes

### Indexing

Items live in a single `map[string]Item` keyed by ID, giving O(1) enqueue,
cancel and existence checks. The canonical ready order (Priority desc,
ReadyAt asc, ID asc) is not maintained incrementally; `Pop` and `Snapshot`
collect matching items and sort on read. This keeps writes cheap and the
state model identical across `Apply`, `Stats`, `Clone` and `Preview`.

### Candidate transactions (Preview)

`Preview` takes one linearizable snapshot of the state under the queue
mutex, deep-copies it into a candidate queue, and runs the real `Apply`
transaction on that candidate. The returned `Result`, `Snapshot` and
`Stats` are therefore exactly what a committed `Apply` on the same state
would produce, with identical error values and priority. The receiver's
state, generation, revision counter and logical clock are never touched;
on failure all three return values are zero.

### Ownership

Every value crossing the API boundary is owned by the receiver of the
call: `Snapshot`, `Pop` and `Preview` return freshly allocated slices, and
`Clone` copies the item map so the clone shares no memory with the
original. Mutating a returned slice or a cloned queue never affects the
source queue. All public methods are serialized by a single mutex, so any
mix of concurrent calls is safe and each observes a consistent state.

### Complexity

- `New`, `Stats`: O(1).
- `ValidateBatch`: O(k) in the number of ops, no state access.
- `Apply`: O(n + k) — one defensive map copy of n items for rollback plus
  k O(1) op executions; capacity is checked once at the end.
- `Pop`: O(n + r log r) where r ≤ n items are ready.
- `Snapshot`: O(n log n) for the canonical sort.
- `Clone`: O(n). `Preview`: O(n + k) plus the candidate `Apply`.
