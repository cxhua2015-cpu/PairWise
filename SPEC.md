# Multi-stream reorder buffer specification

## Types and construction

`New` requires positive `MaxStreams`, `MaxBuffered`, `MaxPayloadBytes`, and `MaxStreamBytes`; otherwise it returns `ErrInvalidOptions`. Events use nonempty stream names no longer than `MaxStreamBytes`, sequence numbers in `[1, math.MaxUint64-1]`, and non-nil Payload. Empty Payload is valid.

Each stream starts with `Next == 1`. `Buffered` counts only future events retained in the registry, and payload capacity is the sum of their byte lengths. Released events do not count toward capacity.

## PushBatch

An empty batch is a successful no-op. The method first structurally validates every input event, before inspecting any state. It then applies events in input order to an isolated candidate:

- `Sequence < Next` returns `ErrOldSequence`;
- an already buffered sequence with byte-identical Payload is an idempotent no-op;
- an already buffered sequence with different Payload returns `ErrConflict`;
- `Sequence > Next` buffers a deep copy;
- `Sequence == Next` releases a deep copy, increments `Next`, and repeatedly releases consecutive buffered events.

Ready output is ordered by input processing, with each newly contiguous chain in ascending sequence order. After the full batch, capacity is checked only on the final candidate state. Any semantic or capacity error rolls back the whole batch and returns no ready events. A successful batch increments generation once iff state changed, including creation of a stream or advancement of `Next`.

## Skip and Delete

`Skip(stream, through)` requires a structurally valid stream and `through` in `[1, math.MaxUint64-1]`. A missing stream returns `ErrNotFound`. If `through < Next`, it is a successful no-op. Otherwise it discards buffered events through that sequence, sets `Next=through+1`, then releases any consecutive buffered suffix. Returned events are in ascending sequence order. A changing Skip increments generation once.

`Delete(stream)` validates the stream, returns `ErrNotFound` when absent, and returns `ErrNotEmpty` if it has buffered events. Otherwise it deletes the stream and increments generation once.

## Snapshot and concurrency

`Snapshot` reports generation, stream count, total buffered events and bytes, and streams sorted by name. Within each stream, buffered events are sorted by sequence. All accepted inputs, returned ready events, and snapshot Payload values are deep copies with no shared mutable backing storage.

All public methods are safe for concurrent use. A mutex plus per-stream hash maps is acceptable. Push is expected O(batch + released) average before cloning for isolation; Skip and Snapshot may sort and take O(n log n). Space is O(streams + buffered payload bytes).
