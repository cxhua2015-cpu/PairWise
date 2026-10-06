# metacatalog261

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

- `servicecatalog.go` — core store and atomic batch transaction engine.
- `validation.go` — side-effect-free structural precheck (`ValidateBatch`), shared with `Apply`.
- `stats.go` — linearizable state summary (`Stats`).
- `clone.go` — ownership-isolating deep copy (`Clone`) plus shared copy helpers.

## Design notes

**Index.** Records live in a `map[string]Record` keyed by name, giving O(1)
average lookup for `Get` and per-op apply. `Snapshot` and `Result.Changed`
collect keys and sort them by name (O(n log n)) so output order is
deterministic.

**Candidate transactions.** `Apply` first runs the shared structural
`ValidateBatch` (no state reads), then takes the write lock and applies the
ops in input order onto a freshly copied candidate map. Puts allocate
consecutive revisions from the local `nextRevision` counter; deletes allocate
none. Record-count and total-value-byte capacities are checked only against
the finished candidate. Any failure (`ErrNotFound`, `ErrCapacity`) discards
the candidate, so records, generation, and revision are never partially
mutated. Only on success are the candidate map and logical clocks swapped in,
and a non-empty successful batch increments `generation` exactly once.

**Ownership.** Every `[]byte` crossing the API boundary is copied: input
values are cloned on Put, and `Get`/`Snapshot`/`Result.Changed` return deep
copies, so callers can never alias or mutate internal state. `Clone` copies
the map, all values, and both logical clocks (`generation`, `nextRevision`)
into a fully independent store.

**Concurrency.** A single `sync.RWMutex` guards all state: `Apply` takes the
write lock, while `Get`, `Snapshot`, `Stats`, and `Clone` take the read lock.
`ValidateBatch` is pure and needs no lock. `Stats` is computed under the read
lock, making it linearizable with concurrent transactions.

**Complexity.** For a batch of k ops over n records with total value bytes B:
structural validation O(k·nameLen); apply O(n + B) for the candidate copy
plus O(k) per-op work; final capacity check O(n). `Get` O(valueLen) for the
defensive copy; `Snapshot`/`Clone` O(n log n + B); `Stats` O(n).
