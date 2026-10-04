# Circuit breaker registry specification

`New` requires positive `MaxServices` and `MaxNameBytes`, and 1..MaxServices unique service policies. Names are nonempty, at most `MaxNameBytes` bytes, and contain only ASCII letters, digits, dot, underscore, slash, or hyphen. Each policy requires positive `FailureThreshold`, `RecoveryThreshold`, and `OpenFor`.

The registry uses explicit nonnegative `int64` time and starts at zero. Every timed method requires `now >= Snapshot.Now`; otherwise it returns `ErrTime`. Adding `OpenFor` must saturate at `math.MaxInt64` rather than overflow.

Services start Closed. In Closed, Success resets consecutive failures; Failure increments them and reaching `FailureThreshold` opens the breaker until `now+OpenFor`, resetting both counters. At `now >= OpenUntil`, Open automatically becomes HalfOpen and resets counters. In HalfOpen, a Failure immediately reopens; Success increments recovery successes and reaching `RecoveryThreshold` closes the breaker. Events cannot be recorded while still Open and return `ErrOpen`.

`Record` first structurally validates the batch and all event service names in input order without reading state. It then checks time, clones all states, advances every due Open service to HalfOpen, and processes events sequentially. Unknown services return `ErrNotFound`. Any error rolls back automatic transitions, time, generation, counters, and all earlier events. A successful nonempty batch increments registry generation once. A successful empty batch increments generation only if automatic transitions occurred. `Result.Changed` lists services whose state changed, without duplicates, sorted by name.

`Allow(service,now)` validates input, atomically advances all due services and time, and returns whether the named service is Closed or HalfOpen plus its state. `Sweep(now)` performs the same global advancement and returns sorted changed services. Allow/Sweep increment generation once only if at least one state changes. `Snapshot` returns services sorted by name. All methods are concurrency-safe.
