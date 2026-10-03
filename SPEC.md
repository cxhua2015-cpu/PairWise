# Idempotency registry specification

## Construction and validation

`New` requires `MaxEntries > 0`, `MaxResultBytes >= 0`, and `MaxKeyBytes > 0`; otherwise it returns `ErrInvalidOptions`. Public string arguments are measured in bytes. Empty keys/fingerprints, keys longer than `MaxKeyBytes`, and negative times or durations are structurally invalid. Validation precedes lookup, token checks, expiry handling, and capacity checks.

## Begin

`Begin(key, fingerprint, now, lease)` requires `lease > 0` and returns one of three observations:

- absent record, or a pending record with `now >= LeaseUntil`: create/replace it as pending and return `Leader=true` with a fresh nonzero monotonically increasing token;
- unexpired pending record with the same fingerprint: return `Pending=true` and its lease deadline;
- completed record with the same fingerprint and `now < ReplayUntil`: return `Replay=true` with a deep copy of its result;
- an unexpired record with another fingerprint: return `ErrConflict`.

A completed record with `now >= ReplayUntil` behaves as absent. A successful takeover or replacement is one mutation and increments generation once. Observation-only calls do not mutate generation. `now+lease` must not overflow `int64`; overflow returns `ErrInvalidTime` without mutation. Capacity is checked against the final replacement state; failure returns `ErrCapacity` without consuming a token.

## Leader operations

`Renew(key, token, now, lease)` requires valid structure and positive token/lease. It succeeds only for the current pending token and only when `now < LeaseUntil`; it sets `LeaseUntil=now+lease`. `Complete(key, token, result, now, replayTTL)` requires `replayTTL > 0`, a result no larger than `MaxResultBytes`, the current pending token, and `now < LeaseUntil`; it atomically changes the record to completed, deep-copies result, and sets `ReplayUntil=now+replayTTL`. `Abort(key, token)` deletes the current pending record. Wrong, stale, or completed tokens return `ErrStaleToken`; `Renew` and `Complete` also return it when their supplied `now` is at or beyond the lease deadline. Each successful state change increments generation once. Failed calls leave all state untouched. Time addition overflow returns `ErrInvalidTime`.

## Sweep and snapshot

`Sweep(now, limit)` requires nonnegative `now` and `limit`; negative `now` returns `ErrInvalidTime`, while negative `limit` returns `ErrInvalidOptions`. It deletes expired pending records (`now >= LeaseUntil`) and expired completed records (`now >= ReplayUntil`) in ascending key order, stopping after `limit` deletions; `limit == 0` means unlimited. It returns deleted keys in that order and increments generation once iff at least one record was deleted.

`Snapshot` returns generation, next token, entry count, result-byte count, and all records sorted by key. Pending records expose no result and completed records expose no lease deadline. Every returned byte slice must be independent from internal state and other return values.

All public methods must be safe for concurrent use. A straightforward implementation may use one mutex; expected operation time is O(1) average except `Sweep` and `Snapshot`, which may sort and take O(n log n). Space is O(entries + result bytes).
