# Barrier registry specification

`New` requires positive `MaxBarriers`, `MaxPending`, and `MaxNameBytes`. It accepts 1..MaxBarriers barrier configurations. Barrier names are unique valid names and `Parties` is positive. Names and participant IDs are nonempty, at most `MaxNameBytes` bytes, and contain only ASCII letters, digits, dot, underscore, slash, or hyphen.

`Apply` first structurally validates every operation in input order without reading state. Arrive and Cancel both require a valid configured barrier and valid participant field shape; unknown kinds or nonzero/extra fields return `ErrInvalidInput`. Whether the barrier exists is a semantic check performed only after all structure validation succeeds.

After validation, `Apply` clones registry state and executes operations sequentially. Arrive adds a participant to the current barrier generation; arriving twice in the same open generation returns `ErrConflict`. When pending arrivals reach exactly `Parties`, that barrier completes: participant IDs are sorted, a `Completion` is emitted with the barrier's current generation (starting at 1), pending arrivals are cleared, and that barrier generation increments. A later operation in the same batch may therefore arrive the same participant in the next generation. Cancel removes a pending participant; a missing arrival returns `ErrNotFound`.

The final total number of pending arrivals across all barriers is checked only after every operation. Exceeding `MaxPending` returns `ErrCapacity`. Any error rolls back all barriers, completions, generations, and pending arrivals. A successful nonempty batch increments the registry generation exactly once; an empty batch does not. Result completions stay in operation occurrence order, while each completion's participants are sorted.

`Snapshot` returns barriers sorted by name and each pending participant list sorted. Returned slices are ownership-isolated. All methods are concurrency-safe.
