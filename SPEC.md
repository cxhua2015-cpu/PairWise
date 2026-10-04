# Lease pool specification

`New` requires positive `MaxPools`, `MaxLeases`, `MaxNameBytes`, and `MaxOwnerBytes`. It accepts 1..MaxPools pool configurations. Pool names are unique valid names and capacities are positive. Names and lease IDs are nonempty, at most `MaxNameBytes` bytes, and contain only ASCII letters, digits, dot, underscore, slash, or hyphen. Owners are nonempty, at most `MaxOwnerBytes` bytes, and use the same alphabet.

The registry uses explicit nonnegative `int64` time and starts at zero. Each timed method requires `now >= Snapshot.Now`; otherwise it returns `ErrTime`. A lease expires exactly when `ExpiresAt <= now`.

`Apply` first structurally validates every operation in input order without reading state. Acquire requires valid Pool, LeaseID, Owner, positive Weight, and `ExpiresAt > Batch.Now`. Release requires only LeaseID; all other fields are zero values. Renew requires only LeaseID and `ExpiresAt > Batch.Now`. Unknown kinds or malformed fields return `ErrInvalidInput`.

After validation and the time check, `Apply` clones state, expires all due leases, then executes operations sequentially. Acquire requires an existing pool and unused lease ID; duplicate IDs return `ErrConflict`. It fails with `ErrCapacity` if the weight exceeds the pool's remaining capacity or if live leases would exceed `MaxLeases`; unsigned arithmetic must not overflow. Release and Renew of a missing or already expired lease return `ErrNotFound`. Renew changes only expiry. Any failure rolls back expiration, time, generation, usage, and all operations.

A successful batch commits `Now=Batch.Now`. It increments generation once if it expired anything or contains at least one operation; a successful empty batch that only advances time does not. `Result.Expired` contains expired lease IDs sorted lexicographically.

`Sweep(now)` atomically expires due leases, advances time, and increments generation once only when at least one lease expires. `Snapshot` returns pools sorted by name and leases sorted by lease ID. All methods are concurrency-safe.
