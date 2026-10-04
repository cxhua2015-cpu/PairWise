# Window counter specification

`New` requires positive `Window`, `MaxKeys`, `MaxEvents`, and `MaxNameBytes`. Keys are nonempty, at most `MaxNameBytes` bytes, and contain only ASCII letters, digits, dot, underscore, slash, or hyphen.

The registry uses explicit nonnegative `int64` time and starts at zero. Every timed method requires `now >= Snapshot.Now`; otherwise it returns `ErrTime`. An event is retained exactly while `event.At > now-Window`; when `now < Window`, no nonnegative event can expire. Implementations must avoid subtraction and sum overflow.

`Apply` first structurally validates the batch and every delta in input order without reading state. Each delta requires a valid key and nonzero `Amount`. After validation and the time check, it clones state, expires old events, then applies deltas sequentially at `Batch.Now`. Each delta appends a distinct event. The live sum for a key may never become negative and signed addition must not overflow; otherwise return `ErrUnderflow` or `ErrOverflow`. Final live key and event counts are checked only after all deltas, allowing a batch to expire old data before adding replacements. Any failure rolls back expiration, time, generation, sums, and events.

A successful batch commits `Now=Batch.Now`. It increments generation once if it expired anything or contains at least one delta; a successful empty batch that only advances time does not. `Result.ExpiredEvents` is the number removed and `Result.Counts` contains the touched keys' final counts sorted by key.

`Get(key,now)` validates input, atomically expires old events, advances time, and returns the live count plus whether the key has retained events. `Sweep(now)` performs the same expiration and returns removed event count. Get/Sweep increment generation once only if at least one event expires. `Snapshot` returns keys sorted and includes event counts. Returned slices are ownership-isolated. All methods are concurrency-safe.
