# configstack

A concurrency-safe, in-memory layered configuration stack for Go 1.22+.
Standard library only. See `SPEC.md` for the authoritative contract.

## Indexing

The stack keeps an ordered slice of layers plus a `map[string]*layerState`
index for O(1) name lookup. Each layer holds its entries in a
`map[string]entryState` for O(1) key lookup. Maps are rebuilt only inside a
committed `Apply`; readers share the immutable committed state under an
`sync.RWMutex`.

## Ordering

Layer position defines priority: index 0 is the highest priority. `AddLayer`
and `Move` insert at an explicit position, shifting lower-priority layers
down. `Resolve` scans layers from index 0 upward and returns the first hit.
`Snapshot` preserves layer order and sorts each layer's entries by key;
`Result.Changed` is sorted by layer priority, then key.

## Transactions

`Apply` is atomic and three-phased:

1. **Structural validation** of every op (kind, name/key charset and byte
   limits, field presence, static position range) before any state is read.
2. **Execution** in input order on an isolated candidate deep copy of the
   layer list. Semantic checks (exists/not-found, dynamic position bounds)
   happen here; each `Set` allocates one revision.
3. **Final capacity check** of layer count, entry count and total live value
   bytes against the configured maxima.

Any failure discards the candidate: nothing is mutated, and neither
`generation` nor `revision` is consumed. A successful nonempty batch commits
the candidate and increments `generation` exactly once.

## Ownership

All `[]byte` values are deep-copied on the way in (`Apply`) and on the way
out (`Resolve`, `Snapshot`, `Result.Changed`). Callers may freely reuse or
mutate buffers they passed in, and may mutate returned values without
affecting the stack.

## Complexity

Let L = layers, E = total entries, B = batch size.

- `Resolve`: O(L) worst case (first-hit scan), O(1) per layer via map.
- `Apply`: O(B + E) — structural validation per op, one full candidate copy
  of the layer list, and a final capacity tally. Map inserts/lookups are
  O(1) amortized; `AddLayer`/`Move` slice shifts are O(L).
- `Snapshot`: O(E log E) for per-layer key sorting, plus O(E) copying.
- Memory: O(E) live entries plus one transient candidate copy per `Apply`.

## Verification

```
go vet ./...
go test -race ./...
go run ./cmd/demo
```
