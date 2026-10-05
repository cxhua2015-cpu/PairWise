# Dedup cache specification

`New` requires positive `MaxEntries`, `MaxKeyBytes`, `MaxTokenBytes`, `MaxValueBytes`, and `MaxTotalValueBytes`; MaxValueBytes may not exceed total. Keys and tokens are nonempty ASCII letters, digits, dot, underscore, slash or hyphen within their limits.

`Apply` validates the whole batch before reading state. Now is nonnegative. Put requires Key, Token, non-nil Value no larger than MaxValueBytes, and ExpiresAt > Now. Delete requires Key and empty Token, nil Value and zero ExpiresAt. Unknown kinds or extra fields return `ErrInvalidInput`. Only then is global monotonic time checked.

Execution clones state and first removes records with `ExpiresAt <= Now`. Operations then run in input order. Put of an absent key creates a record, deep-copies Value, allocates one revision and reports Created. Put of a live key with the same Token reports Replay and returns the stored record unchanged; supplied Value and expiry do not replace it and no revision is allocated. A different token returns ErrConflict. Delete requires a currently present record and removes it without allocating a revision.

Final entry count and total live Value bytes are checked only after all operations. Any error rolls back pruning, operations, time, generation and revisions. Every successful Apply advances time. Generation increments once when pruning or an operation changes state; replay-only and empty batches do not increment it. Outcomes preserve operation order. Result.Revision is the latest committed revision or zero.

`Get(now,key)` validates inputs, requires monotonic time, removes all expired records globally, advances time, and returns a deep copy. Expiry removal increments generation once; Get itself does not. Snapshot returns records sorted by Key with isolated values. All methods are concurrency-safe.
