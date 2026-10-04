# Token bucket specification

## Construction and state

`New` requires `MaxBuckets` and `MaxNameBytes` to be positive. It requires between 1 and `MaxBuckets` specs. Bucket names are nonempty, unique, at most `MaxNameBytes` bytes, and contain only ASCII letters, digits, dot, underscore, or hyphen. `Capacity`, `RefillTokens`, and `RefillEvery` are positive int64 values. Any violation returns `ErrInvalidOptions`.

Every bucket starts full at time zero with `Tokens=Capacity`, `LastRefill=0`, and `LastObserved=0`. Specs are immutable after construction.

## Refill and ApplyBatch

At explicit time `at`, a bucket receives `floor((at-LastRefill)/RefillEvery) * RefillTokens`, capped at Capacity. `LastRefill` advances to the latest complete refill boundary even when the bucket is already full. `LastObserved` advances to `at`. Implementations must avoid overflow: saturation may be decided before multiplication, and the new refill boundary can be computed as `at - elapsed%RefillEvery`.

An empty batch returns the current generation. Every change in a nonempty batch is structurally validated before any state lookup: kind must be Acquire or Refund, bucket name must be structurally valid, Tokens must be positive, and At must be nonnegative. Structural failures return `ErrInvalidInput`.

After validation, changes execute in input order on an isolated candidate. An unknown bucket returns `ErrNotFound`. `At < LastObserved` returns `ErrTimeBackwards`. Each change first applies refill at At. Acquire then subtracts Tokens or returns `ErrInsufficient`; Refund adds Tokens or returns `ErrOverflow` if the result would exceed Capacity. Any failure rolls back every bucket, time field, and refill. A successful nonempty batch increments generation exactly once.

## Inspect, Sweep, and Snapshot

`Inspect(bucket, at)` validates the name and nonnegative time, requires a configured bucket and `at >= LastObserved`, then returns the state that would result from refill at At without mutation. `NextRefill` is zero when the bucket is full or when the next boundary is outside the int64 domain; otherwise it is `LastRefill + RefillEvery`.

`Sweep(at)` requires nonnegative At and `at >= LastObserved` for every bucket. It advances every bucket using the same refill rules. It increments generation exactly once iff at least one bucket's Tokens, LastRefill, or LastObserved changes; a no-op returns the current generation.

`Snapshot` returns generation and all live bucket states ordered by bucket name. Inspect and Snapshot return values are detached value data.

All public methods are concurrency-safe. A mutex and a map keyed by bucket name are acceptable. ApplyBatch may clone all bucket states, using O(number of buckets) time and space.
