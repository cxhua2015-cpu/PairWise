# metacatalog296

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

## Design notes

- **Index**: records live in a `map[string]Record` keyed by name, guarded by a single `sync.RWMutex`. Reads (`Get`, `Snapshot`, `Stats`, `Clone`) take the read lock; `Apply` takes the write lock. A running `totalValue` counter makes capacity checks O(1).
- **Candidate transaction**: `Apply` first runs the shared structural validation (`ValidateBatch`) without touching state, then replays the batch in input order against a scratch copy of the index. Puts allocate consecutive revisions; deletes allocate none and must hit an existing candidate record. Record-count and total-value capacity are checked only against the final candidate state. On any failure the scratch copy is discarded, so records, generation and revision roll back for free; on success the candidate is swapped in and a non-empty batch bumps `generation` exactly once.
- **Ownership**: all values are copied on the way in (`Apply`) and on the way out (`Get`, `Snapshot`, `Result.Changed`, `Clone`), so callers can never alias or mutate internal state. `Clone` preserves the logical clocks (`generation`, `revision`) while sharing nothing with the source.
- **Complexity**: `Apply` is O(n + r) for n ops and r existing records (candidate copy), `Get`/`Stats` are O(1) plus the value copy, `Snapshot`/`Clone` are O(r) with an O(r log r) name sort for `Snapshot`, and `ValidateBatch` is O(n) with no state access.

## Verification

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
