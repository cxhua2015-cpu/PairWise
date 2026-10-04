# Ledger specification

`New` requires positive `MaxAccounts` and `MaxNameBytes`. Account names are nonempty, bounded, and contain only ASCII letters, digits, dot, underscore, slash, or hyphen.

`Apply` structurally validates every operation before reading state. Open requires only Account and nonnegative Amount as its initial balance. Credit and Debit require Account, positive Amount, and empty Other. Transfer requires Account, a distinct valid Other, and positive Amount. Close requires Account, zero Amount, and empty Other. Unknown kinds or malformed fields return `ErrInvalidInput`.

Operations execute sequentially on an isolated candidate. Open requires a missing account. Credit/Debit/Transfer/Close require existing accounts; Transfer also requires Other. Duplicate Open returns `ErrConflict`. Debit and Transfer cannot make a balance negative (`ErrFunds`); Credit and transfer destination addition cannot overflow int64 (`ErrOverflow`). Close requires zero balance (`ErrBalance`). Every successful operation consumes one consecutive revision starting at 1; modified surviving accounts receive that operation revision, and both sides of Transfer receive the same revision. Final account count is checked only after all operations. Any error rolls back accounts, balances, generation, and revision allocation.

A successful nonempty batch increments generation once; an empty batch does not. `Result.Changed` contains touched surviving accounts sorted by name without duplicates. `Get` validates names. `Snapshot` is sorted by name. All methods are concurrency-safe.
