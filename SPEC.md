# Interval allocator specification

`New` requires positive `Size`, `MaxAllocations`, and `MaxNameBytes`. The managed address space is the half-open interval `[0,Size)`. Names are nonempty, at most `MaxNameBytes` bytes, and contain only ASCII letters, digits, dot, underscore, slash, or hyphen.

`Apply` first structurally validates every operation in input order without reading state. Reserve requires a valid Name, positive Length, zero Alignment, and a range `[Start,Start+Length)` wholly inside the address space without unsigned overflow. Allocate requires a valid Name, zero Start, positive Length, and a positive power-of-two Alignment. Free requires only Name and all numeric fields zero. Unknown kinds or malformed fields return `ErrInvalidInput`.

After validation, operations execute sequentially on an isolated candidate. Reserve and Allocate require an unused name. Reserve must not overlap an existing allocation. Allocate selects the lowest aligned start whose full range is free; if none exists it returns `ErrNoSpace`. Duplicate names or overlaps return `ErrConflict`. Free of a missing name returns `ErrNotFound`. Reserve and Allocate receive consecutive revisions starting at 1; Free allocates none. Final allocation count is checked only after all operations. Any error rolls back state, generation, and revision allocation.

A successful nonempty batch increments generation once; an empty batch does not. `Result.Revision` is the latest allocated revision, or zero before any Reserve/Allocate. `Result.Changed` contains touched allocations that survive the batch, sorted by Start then Name, without duplicates.

`Find(length,alignment)` validates positive length and power-of-two alignment and returns the same lowest free start without modifying state. `Lookup` validates a name. `Snapshot` returns allocations sorted by Start then Name. Returned slices are ownership-isolated and all methods are concurrency-safe.
