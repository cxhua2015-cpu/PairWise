# topologygraph428

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

## Design notes

### Indexes

`topologyState` keeps three redundant indexes over the same committed graph:

- `nodes`: set of node names for O(1) existence checks.
- `edges`: set of `edgeKey{from, to}` for O(1) edge lookup and exact counts.
- `out`: adjacency map `from -> {to}` used by cycle detection (`reaches`) and `Reachable`.

All three are mutated together inside one candidate transaction, so they can never diverge on a committed state.

### Candidate transactions

`Apply` and `Preview` never mutate the committed state in place. Under the write lock they clone the current `topologyState` into a throwaway candidate, run the full batch (structural validation has already happened in `ValidateBatch`), and check node/edge capacity only at batch end. On any error the candidate is discarded — atomic rollback for free. On success `Apply` swaps the candidate in and bumps `generation` exactly once (non-empty batches only). `Preview` runs the identical transaction on its own candidate and derives the candidate `Result`, `Snapshot` and `Stats` from it, leaving the receiver's state, generation and ownership untouched.

### Ownership and concurrency

A single `sync.RWMutex` guards the `state` pointer and `generation`. The mutex never guards map internals directly: ownership of a `topologyState` is transferred while the lock is held, and a committed state is treated as immutable (only replaced, never mutated). Readers (`Reachable`, `Snapshot`, `Stats`) run under the read lock and observe one linearizable state. `Clone` deep-copies every map so the copy shares no memory with the original; all returned slices are freshly allocated and sorted, so callers cannot alias internal state.

### Complexity

- `ValidateBatch`: O(ops * nameLen), no state access.
- `Apply` / `Preview`: O(N + E) to clone the candidate, plus O(1) map work per op; `AddEdge` adds one O(N + E) DFS for cycle detection; final capacity check is O(1).
- `Reachable`: O(N + E) DFS on the committed snapshot.
- `Snapshot`: O(N log N + E log E) for the stable sorted output.
- `Stats`: O(1). `Clone`: O(N + E).
