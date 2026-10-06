# balanceledger232

A concurrency-safe, in-memory balance ledger for the distributed control
plane. Go 1.22+, standard library only.

## Semantics

- `Apply` executes a batch atomically, in input order: `Add` / `Set` /
  `Delete`. `Add` and `Set` each allocate the next continuous revision.
- int64 overflow is detected **before** arithmetic, and every resulting
  value must satisfy `|v| <= MaxAbsValue`; violations return `ErrValue`.
- Account capacity (`MaxAccounts`) is checked only on the **final** state
  of the batch, so transient over-capacity batches that delete again are
  legal. Any failure rolls the whole batch back.
- `Top(n)` sorts by value descending, then name ascending. `Snapshot`
  sorts by name. Returned slices never alias internal state.
- A successful non-empty batch increments `generation` exactly once;
  empty batches succeed without changing it.
- Names must be non-empty `[a-z0-9-_]` within `MaxNameBytes`; all
  `Options` limits must be positive.

## Architecture

The implementation is split across four cooperating files:

- `creditpool.go` — core transaction engine. Holds the `Ledger` state
  (a `map[string]accountState` index keyed by name, plus the logical
  clocks `generation` and `nextRev`) guarded by a single
  `sync.RWMutex`. Writers take the exclusive lock; `Top`, `Snapshot`,
  `Stats`, `Clone` and `ValidateBatch` run under the read lock, so all
  public methods are safe for concurrent use.
- `validation.go` — side-effect-free structural preflight
  (`ValidateBatch`). It validates kind, name charset/length and
  kind-specific field usage without touching state. `Apply` calls the
  same function, so preflight and execution share one structural
  contract.
- `stats.go` — `Stats` reads generation, next revision and account
  count under the read lock, giving a linearizable summary consistent
  with concurrent transactions.
- `clone.go` — `Clone` copies the map and both logical clocks into a
  new `Ledger` under the read lock. The clone owns its map outright:
  no slices or maps are shared, so later batches on either ledger can
  never alias each other.

## Candidate transactions

`Apply` never mutates live state speculatively. It first clones the
account map into a **candidate** map, replays the ops against it with a
local next-revision counter, and only on success swaps the candidate in
and bumps `generation` once. Any error (`ErrValue`, `ErrNotFound`,
`ErrCapacity`) simply discards the candidate — rollback is free and the
observed state is untouched.

## Ownership

All values crossing the API boundary (`Account`, `Result.Changed`,
`Snapshot.Accounts`, `Top` results) are freshly built per call. Internals
are plain value types (`accountState`), so the map copy in `Clone` and
the candidate map in `Apply` are deep by construction.

## Complexity

Let `n` be the number of accounts and `b` the batch size.

- `Apply`: `O(n + b)` — one map copy plus per-op `O(1)` work.
- `ValidateBatch`: `O(b)`, no allocation, no locking of state.
- `Top(k)`: `O(n log n)` sort, `O(n)` extra space.
- `Snapshot`: `O(n log n)` sort by name.
- `Stats`: `O(1)`.
- `Clone`: `O(n)` time and space.

## Testing

`go test ./...`, `go test -race ./...`, `go run ./cmd/demo`.
