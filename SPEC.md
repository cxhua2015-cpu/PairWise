# Expiring store specification

`New` requires positive `MaxEntries`, `MaxTotalBytes`, `MaxValueBytes`, and `MaxKeyBytes`; `MaxValueBytes` may not exceed `MaxTotalBytes`. Keys are nonempty, at most `MaxKeyBytes` bytes, and contain only ASCII letters, digits, dot, underscore, slash, or hyphen. Values are measured in bytes.

The store uses explicit nonnegative `int64` time and starts at time zero. Every public operation carrying time requires `now >= Snapshot.Now`; otherwise it returns `ErrTime`. An entry is expired exactly when `ExpiresAt <= now`.

`Apply` first structurally validates the batch and every operation in input order without reading state. Put requires a non-nil value no larger than `MaxValueBytes` and `ExpiresAt > Batch.Now`. Delete requires nil Value and zero ExpiresAt. Touch requires nil Value and `ExpiresAt > Batch.Now`. Unknown kinds or malformed fields return `ErrInvalidInput`.

After validation and the monotonic-time check, `Apply` works on an isolated candidate. It first removes every entry expired at `Batch.Now`, then executes operations sequentially. Put creates or replaces a key and receives the current `NextRevision`, starting at 1. Delete and Touch of a missing or already expired key return `ErrNotFound`. Touch changes only expiry. Repeated operations are allowed. Final entry count and total live value bytes are checked after all operations. Any error rolls back expiration, time, generation, state, and revision allocation.

A successful batch commits `Now=Batch.Now`. It increments generation once if it expired anything or contains at least one operation; a successful empty batch that only advances time does not. `Result.Revision` is the latest allocated revision, or zero before any Put. `Result.Expired` is sorted by key.

`Get(key,now)` validates key and time, atomically expires all due entries, advances time, and returns a deep copy. `Sweep(now)` performs the same expiration and returns sorted expired keys. Get/Sweep increment generation once only when at least one entry expires. `Snapshot` returns entries sorted by key. All input and output Value slices are ownership-isolated. All methods are concurrency-safe.
