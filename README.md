# expirytable424

Concurrent-safe, in-memory expiry state table for a distributed control plane.
Go 1.22+, standard library only. See `SPEC.md` for the authoritative contract.

## Architecture

The implementation is split across five cooperating files in `expirytable424/`:

- `heartbeat.go` — core types (`Table`, `Batch`, `Op`, `Entry`, `Result`,
  `Snapshot`), the transaction engine (`Apply`, `Expire`, `Snapshot`) and the
  candidate-state commit helper.
- `validation.go` — side-effect-free structural precheck (`ValidateBatch`),
  shared verbatim by `Apply` and `Preview` so structural semantics can never
  drift between entry points.
- `stats.go` — linearizable `Stats` summary computed under the same lock.
- `clone.go` — deep copy preserving logical clocks with fully disjoint
  ownership.
- `preview.go` — `Preview`, a non-mutating rehearsal of `Apply` on one
  linearizable snapshot.

## Indexing

Entries live in a single `map[string]entry` keyed by the validated key string,
giving O(1) average Put/Touch/Delete and lookup. There is no secondary time
index: expiry sweeps scan the map, which keeps the transaction path simple and
exactly matches the closed-boundary (`ExpiresAt <= Now`) semantics.

## Candidate transactions

`Apply` never mutates the live state in place. It validates the batch
structurally first (no state read), then checks monotonic time under the
mutex, then runs the whole transaction — expiry sweep, ordered ops, revision
allocation, generation bump — on a copied *candidate* state. Only on success
is the candidate swapped in. Capacity overflow, missing keys, or any other
error therefore rolls back evictions, logical time, and allocated revisions
for free, because the receiver's state was never touched. `Preview` reuses the
identical commit path on a cloned state, so its `Result`, candidate `Snapshot`
and candidate `Stats` are exactly what a real commit of the same batch on the
same snapshot would produce, while the receiver's state, generation, revision
counter and logical clock stay untouched.

## Ownership

Every value crossing the API boundary is independently owned: `Snapshot` and
`Expire` build fresh slices, `Clone` copies the entry map, and `Preview`
returns slices derived from its private candidate state. Callers may mutate
returned slices freely without affecting the table. `Clone` preserves the
logical clocks (generation, next revision, Now) but shares no memory — and no
mutex — with the original.

## Concurrency

A single `sync.Mutex` guards all state. Every public method is safe for
concurrent use; `ValidateBatch` is lock-free because it only reads the
immutable `Options`.

## Complexity

- `Apply`: O(k + n) — k ops plus one sweep over n entries.
- `Expire`: O(n + m log m) — sweep plus sorting the m removed keys.
- `Snapshot`: O(n log n) — copy plus key sort.
- `Stats`: O(1). `Clone`: O(n). `Preview`: O(k + n log n).
- Space: O(n) plus one O(n) candidate copy per transaction.
