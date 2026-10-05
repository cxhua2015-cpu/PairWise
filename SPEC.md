# Sliding window limiter specification

`New` requires positive `Window`, `Limit`, `MaxKeys`, `MaxKeyBytes`, and `MaxEventsPerKey`. Keys are nonempty ASCII letters, digits, dot, underscore, slash or hyphen within the byte limit.

`Check(Batch)` validates the entire batch before reading state: `Now >= 0`, every key is valid and every Units is positive and at most Limit. Only then it checks global monotonic time; a lower time returns `ErrTime`. Validation errors take precedence over time errors.

Execution uses an isolated candidate. First remove every stored event with `At <= Now-Window` (the left boundary is excluded). Requests then execute in input order. A request is allowed when the live unit sum plus Units is at most Limit; an allowed request appends an event at Now and allocates one revision. A denied request writes nothing and allocates no revision. Decisions preserve input order and report Used after that decision and Remaining. Events at identical times preserve revision order.

Final distinct nonempty key count and event count per key are checked only after all requests. Capacity failure rolls back pruning, time, generation and revisions. Any successful Check advances time. Generation increments once if pruning removed anything or at least one request was allowed; an empty/no-change batch does not increment it. Result.Revision is the latest committed revision or zero.

Snapshot returns keys lexicographically and each key's events by At then Revision. Returned slices are isolated. All methods are concurrency-safe.
