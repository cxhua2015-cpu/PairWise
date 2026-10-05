# Delayed queue specification

`New` requires positive `MaxJobs`, `MaxIDBytes`, `MaxPayloadBytes`, and `MaxTotalPayloadBytes`; `MaxPayloadBytes` may not exceed `MaxTotalPayloadBytes`. IDs are nonempty, at most `MaxIDBytes` bytes, and contain only ASCII letters, digits, dot, underscore, slash, or hyphen.

The queue uses explicit nonnegative `int64` time and starts at zero. Every timed method requires `now >= Snapshot.Now`; otherwise it returns `ErrTime`.

`Apply` first structurally validates the batch and every operation in input order without reading state. Enqueue requires a valid ID, non-nil Payload no larger than `MaxPayloadBytes`, and `ReadyAt >= Batch.Now`. Reschedule requires a valid ID, nil Payload, and `ReadyAt >= Batch.Now`; it may set any Priority including zero. Cancel requires only ID; Priority and ReadyAt are zero and Payload is nil. Unknown kinds or malformed fields return `ErrInvalidInput`.

After validation and the time check, operations execute sequentially on an isolated candidate. Enqueue requires an unused ID. Reschedule and Cancel require an existing job. Enqueue and Reschedule allocate consecutive revisions starting at 1; Cancel allocates none. Repeated operations are allowed, including cancel then enqueue of the same ID. Final job count and total live Payload bytes are checked only after all operations. Any failure rolls back jobs, time, generation, and revision allocation.

A successful batch commits `Now=Batch.Now`. It increments generation once for a nonempty batch; an empty batch may advance time but does not increment generation. `Result.Revision` is the latest allocated revision, or zero before any allocation. `Result.Changed` contains touched jobs that survive the batch, sorted by ID without duplicates.

`Peek(now,limit)` and `Take(now,limit)` require nonnegative monotonic time and limit 1..1000. They consider jobs ready when `ReadyAt <= now` and order them by Priority descending, ReadyAt ascending, then ID ascending. Peek advances time and returns deep copies without removing jobs or changing generation. Take advances time, removes up to limit ready jobs atomically, and increments generation once only when it removes at least one job. Neither method consumes revisions. `Snapshot` returns jobs sorted by ID. All Payload slices are ownership-isolated and all methods are concurrency-safe.
