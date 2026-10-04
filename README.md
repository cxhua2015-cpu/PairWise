# leasepool

See `SPEC.md`. Implement the `leasepool` package without changing the public contract.

## Implementation notes

### Indexing
The registry keeps two in-memory maps guarded by a single `sync.Mutex`:
- `pools`: pool name → `{capacity, used, leases}` (per-pool capacity accounting).
- `leases`: lease ID → `Lease` (global uniqueness of lease IDs across pools).

`Snapshot` materializes sorted copies (pools by name, leases by lease ID) so
callers can mutate results without affecting registry state.

### Batch transactions
`Apply` runs in three phases under the lock:
1. **Structural validation** of every op in input order, without reading state
   (kind-specific field shapes, name/owner alphabets, positive weight,
   `ExpiresAt > Batch.Now`). Any malformed op yields `ErrInvalidInput`.
2. **Time check**: `Batch.Now >= now`, else `ErrTime`.
3. **Candidate execution**: the pools and leases maps are cloned, due leases
   (`ExpiresAt <= now`) are expired in the clone, then ops execute in input
   order. Any failure (`ErrNotFound`, `ErrConflict`, `ErrCapacity`) discards
   the clone, so expiration, time, generation, and usage never leak. On success
   the clone is committed atomically and `Now` advances.

Generation increments once per successful batch that expired anything or
contained at least one op; a successful empty time-advancing batch does not
increment it.

### Expiration
Expiry is lazy and exact: a lease is expired when `ExpiresAt <= now`.
Expiration happens at the start of `Apply` (on the candidate state) and in
`Sweep`. `Result.Expired` / `Sweep` return expired lease IDs sorted
lexicographically. `Sweep` increments generation only when at least one lease
expires.

### Capacity accounting
Each pool tracks `used` with the invariant `used <= capacity`. Acquire checks
`weight > capacity - used` (subtraction on a non-negative invariant, so no
`uint64` overflow) before adding. Live-lease count is bounded by `MaxLeases`.
Release and expiration subtract exactly the recorded weight.

### Time
Time is explicit (`int64`), supplied per call, starts at 0, and is monotone
non-decreasing across `Apply` and `Sweep`; regressions return `ErrTime` with
no state change.

### Complexity
Let `P` = pools, `L` = live leases, `B` = batch ops.
- `Apply`: validation `O(B)`, clone `O(P + L)`, expiration `O(L + E log E)`
  for `E` expired, execution `O(B)` → `O(P + L + B + E log E)` overall.
- `Sweep`: `O(P + L + E log E)`.
- `Snapshot`: `O(P log P + L log L)`.
- All operations are serialized by one mutex; no per-lease goroutines or
  timers are used.
