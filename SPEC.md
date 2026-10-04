# Fair queue specification

## Construction and data model

`New` requires positive `MaxTasks`, `MaxPayloadBytes`, and `MaxNameBytes`. It also requires at least one `QueueWeight`. Queue names are nonempty, unique, at most `MaxNameBytes` bytes, and contain only ASCII letters, digits, dot, underscore, or hyphen. Each weight is in `[1,64]`, and the sum of weights is at most 1024. Any violation returns `ErrInvalidOptions`.

The immutable scheduling wheel is constructed by sorting queue names ascending and repeating each name `Weight` consecutive times. For weights `a=2,b=1`, the wheel is `[a,a,b]`. Task IDs follow the same name rules and are globally unique. A task Payload must be non-nil; an empty slice is valid. A single Payload may be at most `MaxPayloadBytes` bytes.

Every successful Put receives the current `NextSequence`, starting at 1, after which the candidate counter increments. Sequence zero is never produced. Tasks within a named queue are FIFO by Sequence.

## ApplyBatch

An empty batch is a successful no-op returning the current generation. Every change in a nonempty batch is structurally validated before any state lookup. Put requires a valid ID, a configured queue, and a valid Payload. Delete requires a valid ID; its Queue and Payload fields must have their zero values. Unknown kinds and malformed deletes return `ErrInvalidInput`. Repeated IDs are permitted and resolved sequentially.

After structural validation, changes execute in input order on an isolated candidate. Put of an existing ID returns `ErrExists`; Delete of a missing ID returns `ErrNotFound`. Delete followed by Put of the same ID is valid and assigns a fresh Sequence. Only the final candidate is checked against `MaxTasks` and total stored Payload bytes. Any failure rolls back the whole batch, including `NextSequence`. A successful nonempty batch increments generation exactly once and deep-copies all added Payload values. ApplyBatch does not change the scheduling cursor.

## Peek, Dequeue, and Snapshot

`Peek(limit)` and `Dequeue(limit)` require `limit` in `[1,1000]`. Starting at the current cursor, scheduling inspects wheel slots in order. Each inspected slot advances the simulated cursor by one modulo wheel length. If that queue is nonempty, its oldest task is selected. If it is empty, scanning continues; after a full wheel scan with no selection, scheduling stops. This repeats until `limit` tasks are selected or all queues are empty.

`Peek` returns the selected tasks and resulting simulated cursor without mutation. `Dequeue` performs the same schedule, removes selected tasks, stores the resulting cursor, and increments generation exactly once iff at least one task was removed. A Dequeue on an empty table returns the unchanged cursor and generation.

`Snapshot` reports generation, next sequence, cursor, task count, Payload bytes, the immutable wheel, and all live tasks ordered by Queue ascending then Sequence ascending. All returned slices and Payload values are independent from inputs, internal state, and other return values.

All public methods are concurrency-safe. A mutex, global ID map, and per-queue FIFO slices are acceptable. Batch cloning may use O(total tasks + Payload bytes); ordered snapshots may sort in O(n log n).
