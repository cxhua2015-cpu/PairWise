# Prefix claim specification

`New` requires positive `MaxClaims`, `MaxPathBytes`, and `MaxOwnerBytes`. Paths are canonical absolute slash paths: `/` is valid; otherwise they start with `/`, have no trailing slash, no empty segment, and segments contain only ASCII letters, digits, dot, underscore, or hyphen. Owners are nonempty, bounded by `MaxOwnerBytes`, and use letters, digits, dot, underscore, slash, or hyphen.

`Apply` structurally validates every operation before reading state. Claim requires valid Path and Owner. Release requires valid Path and Owner. Unknown kinds or malformed input returns `ErrInvalidInput`.

Operations then execute sequentially on an isolated candidate. Claim conflicts with every existing equal, ancestor, or descendant path, regardless of owner, and returns `ErrConflict`; otherwise it allocates the current revision starting at 1. Release requires the exact path and matching owner, returning `ErrNotFound` or `ErrOwner` otherwise, and allocates no revision. A release followed by a claim in one batch is allowed. Final claim count is checked only after all operations. Any failure rolls back state, generation, and revision allocation.

A successful nonempty batch increments generation once. `Result.Revision` is the latest allocated revision. `Result.Changed` contains touched surviving claims sorted by Path without duplicates. `Lookup(path)` returns the exact or nearest ancestor claim; `Descendants(path,after,limit)` returns claims strictly below path, lexicographically after `after`, sorted by Path, with limit 1..1000. Empty `after` is valid; nonempty after must be a valid path. `Snapshot` is sorted by Path. Returned slices are isolated and all methods are concurrency-safe.
