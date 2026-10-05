# Lease pool specification

`New` requires positive `MaxResources`, `MaxNameBytes`, and `MaxOwnerBytes`. Resource and owner names are nonempty, within their byte limits, and contain only ASCII letters, digits, dot, underscore, slash, or hyphen.

The pool starts at time zero. `Apply` first structurally validates the complete batch without reading state. `Batch.Now` must be nonnegative. Add and Remove require only Resource. Acquire and Renew require Resource, Owner, and `ExpiresAt > Batch.Now`. Release requires Resource and Owner with zero ExpiresAt. Unknown kinds or extra fields return `ErrInvalidInput`. Only after validation is `Batch.Now >= Snapshot.Now` checked; otherwise `ErrTime`.

Operations execute in input order on an isolated candidate. Add requires an absent resource. Remove requires an existing unleased resource; an expired lease still occupies the resource until Acquire or Expire removes it. Acquire requires an existing resource and succeeds when no lease exists or its `ExpiresAt <= Batch.Now`; it installs the requested lease. Renew requires a live lease owned by Owner. Release requires a live lease owned by Owner. Owner mismatch returns `ErrOwner`; missing or expired leases return `ErrNotFound`; acquiring a live lease returns `ErrBusy`.

Every successful operation allocates one consecutive revision beginning at 1. Acquire and Renew store that revision on the lease. Final resource count is checked only after the full batch. Any error rolls back all state, time, generation and revisions. A successful nonempty batch increments generation once and advances time; an empty batch only advances time. `Result.Changed` contains surviving leases touched by Acquire/Renew, sorted by Resource without duplicates. `Result.Revision` is the latest committed revision or zero.

`Expire(now)` requires nonnegative monotonic time. It removes leases with `ExpiresAt <= now`, returns them sorted by Resource, and increments generation once only when at least one lease is removed. It allocates no revisions. `Snapshot` returns Resources and Leases sorted by Resource. All methods are concurrency-safe and returned slices do not alias internal storage.
