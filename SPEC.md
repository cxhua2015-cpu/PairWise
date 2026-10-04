# Scoreboard specification

`New` requires positive `MaxMembers` and `MaxNameBytes`. Member names are nonempty, at most `MaxNameBytes` bytes, and contain only ASCII letters, digits, dot, underscore, slash, or hyphen.

`Apply` first structurally validates every operation in input order without reading state. Upsert requires a valid Member, any Score, and zero Delta. Increment requires a valid Member, zero Score, and nonzero Delta. Delete requires a valid Member and zero Score/Delta. Unknown kinds or malformed fields return `ErrInvalidInput`.

After validation, operations execute sequentially on an isolated candidate. Upsert creates or replaces and allocates the current `NextRevision`, starting at 1. Increment requires an existing member, adds Delta without signed overflow, and allocates a revision. Delete requires an existing member and allocates no revision. Repeated operations are allowed. Final member count is checked only after all operations, so deleting before creating may free capacity. Any error rolls back state, generation, and revision allocation.

A successful nonempty batch increments generation once; an empty batch does not. `Result.Revision` is the latest allocated revision, or zero before any Upsert/Increment. `Result.Changed` contains touched members that exist in the final state, sorted by score descending and member ascending, with no duplicates.

`Get` validates a member. `Range(min,max,cursor,limit)` requires `min <= max`, limit 1..1000, and either an unset zero cursor or a set cursor with valid member. It selects inclusive scores and orders entries by score descending then member ascending. A set cursor returns only entries strictly after `(Score,Member)` in that ordering, even if the cursor does not currently exist. `Snapshot` uses the same ordering. Returned slices are ownership-isolated and all methods are concurrency-safe.
