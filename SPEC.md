# Rank board specification

`New` requires positive `MaxItems`, `MaxIDBytes`, and `MaxAbsScore`. IDs are nonempty valid ASCII letters, digits, dot, underscore, slash or hyphen within the byte limit.

`Apply` validates the complete batch before reading state. Add requires valid ID, nonzero Delta, and zero Score. Set requires valid ID, zero Delta, and Score within [-MaxAbsScore, MaxAbsScore]. Delete requires valid ID and zero numeric fields. Unknown kinds or extra fields return ErrInvalidInput.

Operations execute in order on an isolated candidate. Add creates a missing item at zero then adds Delta; Set inserts/replaces. Both allocate one consecutive revision. Delete requires an existing item and allocates none. Add must detect int64 overflow before arithmetic, then enforce the absolute score limit; Set enforces the same limit during validation. Arithmetic violations return ErrScore.

Final item count is checked only after all operations. Any failure rolls back state, generation and revisions. A successful nonempty batch increments generation once; an empty batch changes nothing. Result.Changed contains surviving items touched by Add/Set, without duplicates, sorted by ID. Result.Revision is the latest committed revision or zero.

`Top(limit)` requires limit 1..1000 and returns up to limit items ordered by Score descending then ID ascending. Snapshot returns all items by ID. Returned slices are isolated and all methods are concurrency-safe.
