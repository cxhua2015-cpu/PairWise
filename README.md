# leasepool

See `SPEC.md`. Implement the `leasepool` package without changing the public contract.

## Design

### Indexing

The pool keeps two hash maps guarded by a single `sync.Mutex`:

- `resources map[string]struct{}` — set of registered resource names.
- `leases map[string]Lease` — live or expired leases keyed by resource name.

Sorted output (`Snapshot`, `Result.Changed`, `Expire`) is produced by sorting
map keys on demand; no ordered index is maintained.

### Candidate transactions

`Apply` runs in three phases under the mutex:

1. **Structural validation** of the whole batch (names, kinds, required/zero
   fields, `ExpiresAt > Now`) without reading state.
2. **Time check**: `Batch.Now >= current now`, else `ErrTime`.
3. **Execution** on an isolated candidate: both maps are copied, ops run in
   input order against the copies, and the final resource count is checked
   only after the last op. Any error discards the candidate, so time,
   generation, and revisions are untouched. On success the candidate replaces
   the committed state; a nonempty batch increments `generation` once and
   advances time, an empty batch only advances time.

### Expiry rules

A lease with `ExpiresAt <= now` is expired. Expired leases still occupy their
resource (`Remove` returns `ErrBusy`) and are invisible to `Renew`/`Release`
(`ErrNotFound`), but `Acquire` treats the resource as free and atomically
replaces the stale lease on commit. `Expire(now)` removes all leases with
`ExpiresAt <= now`, advances time, and increments `generation` only when at
least one lease was removed; it allocates no revisions.

### Revision / generation

Every successful operation in a committed batch allocates one consecutive
revision starting at 1 (`Snapshot.NextRevision` is the next value to hand
out). `Acquire` and `Renew` store their revision on the installed lease.
`generation` counts committed mutations: +1 per nonempty successful batch,
and +1 per `Expire` call that removed at least one lease. Rolled-back batches
allocate neither.

### Complexity

- `Apply`: O(n + R + L) time, O(R + L) extra space per batch, where n is the
  batch size and R/L are the resource/lease counts (candidate copy).
- `Expire`: O(L log L) time (scan plus sort of removed leases).
- `Snapshot`: O(R log R) time, O(R + L) space for the returned slices.
- Storage: O(R + L). All returned slices are freshly allocated and never
  alias internal storage; all public methods are safe for concurrent use.
