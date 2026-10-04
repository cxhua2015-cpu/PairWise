# Range lock manager specification

## Construction and structure

`New` requires positive `MaxLocks`, `MaxOwners`, `MaxMetadataBytes`, and `MaxNameBytes`; otherwise it returns `ErrInvalidOptions`. IDs, owners, and resources are nonempty and no longer than `MaxNameBytes` bytes. Every `AcquireBatch` call requires nonnegative `now`, including an empty batch. A request requires `Start < End`, mode `Read` or `Write`, positive TTL, and `now+TTL` representable as `int64`. Metadata may be nil or empty; each value may be at most `MaxMetadataBytes` bytes.

Every batch is fully structurally validated before state lookup. Duplicate IDs inside one batch return `ErrInvalidInput`. Structural errors never prune expired locks, consume tokens, or mutate generation.

## AcquireBatch

An empty batch is a successful no-op returning the current generation. A nonempty batch runs on an isolated candidate. First it removes every lock with `now >= ExpiresAt`; then it applies requests in input order.

IDs must be globally unique in the candidate. On the same resource, half-open intervals overlap exactly when `a.Start < b.End && b.Start < a.End`. Overlapping `Read` locks coexist; a pair conflicts when either mode is `Write`. Owner identity does not bypass conflicts. Each accepted request receives the candidate's next nonzero monotonically increasing fencing token and `ExpiresAt=now+TTL`.

Only after the whole batch are final lock count, distinct owner count, and total stored metadata bytes checked. Failure returns `ErrExists`, `ErrConflict`, or `ErrCapacity` as applicable, rolls back expiry pruning and all requests, and does not advance `NextToken`. Success deep-copies metadata, increments generation once, and returns leases in request order with independent metadata copies.

## Lease operations

`Renew(id, token, now, ttl)` validates all structure first. It succeeds only for the current token and when `now < ExpiresAt`; it replaces the deadline with `now+ttl` and increments generation once. `Release(id, token)` removes the current matching lock and increments generation once. Missing, wrong-token, or expired-at-the-supplied-time renewals return `ErrStaleToken`. Release has no time argument, so it may release an unswept expired lock if the token still matches. Failed operations are exact no-ops.

## Sweep, Query, and Snapshot

`Sweep(now, limit)` requires nonnegative values. It deletes expired locks in ascending ID order, with `limit==0` meaning unlimited, and increments generation once iff anything was deleted. Negative `now` returns `ErrInvalidTime`; negative `limit` returns `ErrInvalidInput`.

`Query(resource,start,end,now)` validates its inputs and returns unexpired locks on that resource whose intervals overlap the window, sorted by `Start`, then `End`, then ID. Query does not prune or mutate. `Snapshot` includes all stored locks, even unswept expired ones, sorted by resource, `Start`, `End`, then ID, and reports generation, next token, lock count, distinct owner count, and metadata bytes.

All returned metadata is independent from inputs, internal state, and other return values. All public methods are safe for concurrent use. A single mutex and maps are acceptable; batch conflict detection may be O(n²), while Query/Snapshot/Sweep may sort in O(n log n).
