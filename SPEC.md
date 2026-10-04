# Deadline queue specification

## Construction and task structure

`New` requires positive `MaxTasks`, `MaxPayloadBytes`, and `MaxNameBytes`; otherwise it returns `ErrInvalidOptions`. IDs and queue names are nonempty and at most `MaxNameBytes` bytes. Due times are nonnegative `int64`. Payload must be non-nil; an empty slice is valid. A single Payload may be at most `MaxPayloadBytes` bytes.

Tasks are globally unique by ID. Canonical order is queue ascending, Due ascending, Priority descending, then ID ascending. Priority spans the full `int32` range.

## ApplyBatch

An empty batch is a successful no-op returning the current generation. Every change in a nonempty batch is structurally validated before any state lookup. `Add` requires a fully valid Task. `Delete` requires a valid `Task.ID`; every other Task field must have its zero value. Unknown kinds and malformed deletes return `ErrInvalidInput`. Repeated IDs are permitted and are resolved sequentially; this is what allows Delete followed by Add of the same ID.

After structural validation, changes execute in input order on an isolated candidate. Add of an existing ID returns `ErrExists`; Delete of a missing ID returns `ErrNotFound`. Delete followed by Add of the same ID is valid. Only the final candidate is checked against `MaxTasks` and total stored Payload bytes. Any failure rolls back the whole batch. A successful nonempty batch increments generation exactly once and deep-copies all added Payload values.

## PopDue, Window, and Snapshot

`PopDue(queue, now, limit)` requires a valid queue, nonnegative `now`, and `limit` in `[0,1000]`; zero means unlimited. It removes tasks from that queue with `Due <= now`, ordered by Due ascending, Priority descending, then ID ascending, stopping at the limit. It increments generation once iff at least one task is removed.

`Window(queue, start, end, limit)` requires a valid queue, `0 <= start <= end`, and `limit` in `[1,1000]`. It returns tasks with `start <= Due < end` in canonical within-queue order without mutation. An empty window succeeds with an empty result.

`Snapshot` reports generation, task count, Payload bytes, and every task in global canonical order. All returned Payload slices are independent from inputs, internal state, and other return values.

All public methods are concurrency-safe. A mutex plus an ID map is acceptable; batch cloning is O(total tasks), while ordered reads/pops/snapshots may sort in O(n log n). Space is O(task metadata + Payload bytes).

