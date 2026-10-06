# balanceledger237

Concurrency-safe in-memory balance ledger (Go 1.22+, standard library only).
See `SPEC.md` for the normative contract.

## Architecture

The implementation is split across four cooperating files:

- `creditpool.go` — core types, `New`, `Apply`, `Top`, `Snapshot`.
- `validation.go` — side-effect-free structural preflight (`ValidateBatch`),
  shared verbatim with `Apply` so both enforce identical structural semantics.
- `stats.go` — linearizable `Stats` summary read under the ledger lock.
- `clone.go` — ownership-safe deep copy preserving the logical clocks.

## Index

The ledger keeps a single authoritative index: `map[string]Account` guarded by
a `sync.RWMutex`. Writers (`Apply`) take the write lock; readers (`Top`,
`Snapshot`, `Stats`, `Clone`) take the read lock, so all public methods are
safe for concurrent use and every observation is linearizable.

## Candidate transactions

`Apply` never mutates live state speculatively. After structural validation it
copies the account map into a private **candidate**, replays the ops in input
order against it (assigning consecutive revisions to Add/Set, pre-checking
int64 overflow and the absolute-value bound before any arithmetic), and checks
the final account capacity only at batch end. Any failure simply discards the
candidate — rollback is free and the generation/revision clocks stay untouched.
Only a fully successful candidate is swapped in, and a non-empty committed
batch advances `generation` exactly once.

## Ownership

All values crossing the API boundary are copies: `Top` and `Snapshot` build
fresh slices, `Result.Changed` is newly allocated, and `Clone` duplicates the
entire map plus the logical clocks (`generation`, `nextRevision`). Mutating a
returned slice or a clone never aliases the ledger's internal state.

## Complexity

Let `b` = batch size, `n` = number of accounts.

- `Apply`: O(b + n) time (candidate copy), O(b + n) extra space.
- `ValidateBatch`: O(b) time, O(1) space, no state access.
- `Top(k)`: O(n log n) sort, O(n) space; result truncated to `k`.
- `Snapshot`: O(n log n) time, O(n) space.
- `Stats`: O(1). `Clone`: O(n) time and space.
