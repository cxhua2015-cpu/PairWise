# BiMap specification

`New` requires positive `MaxPairs` and `MaxNameBytes`. Left and right names are nonempty, bounded, and contain only ASCII letters, digits, dot, underscore, slash, or hyphen.

`Apply` structurally validates every operation before reading state. Bind and Unbind both require valid Left and Right; unknown kinds return `ErrInvalidInput`. Operations execute sequentially on an isolated candidate. Bind requires both sides to be unused and returns `ErrConflict` otherwise; it allocates the current revision starting at 1. Unbind requires the exact existing pair: a missing side returns `ErrNotFound`, while either side bound to a different counterpart returns `ErrMismatch`; it allocates no revision. An unbind followed by a bind may reuse either side in the same batch.

Final pair count is checked only after all operations. Any error rolls back both indexes, generation, and revision allocation. A successful nonempty batch increments generation once; an empty batch does not. `Result.Changed` contains touched surviving pairs sorted by Left without duplicates, and `Result.Revision` is the latest allocated revision.

`LookupLeft` and `LookupRight` validate input. `List(after,limit)` accepts empty after; nonempty after must be valid, limit is 1..1000, and results are sorted by Left and strictly after the cursor. `Snapshot` is sorted by Left. All methods are concurrency-safe.
