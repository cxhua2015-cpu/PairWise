# Multi-dimensional quota manager specification

## Construction and common validation

`New` requires positive `MaxSubjects`, `MaxReservations`, `MaxMetadataBytes`, and `MaxNameBytes`; otherwise it returns `ErrInvalidOptions`. Subject, dimension, and reservation names are nonempty and at most `MaxNameBytes` bytes. Amounts and limits are nonnegative `int64`; reservation demands must be strictly positive.

Every composite input is fully structurally validated before state lookup. Duplicate `(Subject, Dimension)` pairs in one call are structurally invalid. Validation errors return `ErrInvalidInput` and never mutate state.

## Limits

`SetLimits` takes a nonempty batch and atomically sets the specified limits. Missing subjects are created. Every currently used amount must fit the resulting limit; otherwise `ErrExceeded` rolls back the batch. The final subject count must not exceed `MaxSubjects`. A successful call increments generation once, including setting an existing limit to the same value.

## Reservations

`Reserve(id, demands, metadata)` requires a valid ID, a nonempty demand list, and metadata no larger than `MaxMetadataBytes`. The ID must be absent. Every referenced subject/dimension must be configured. Adding each demand to current usage must not overflow `int64` or exceed its limit. The final reservation count and total stored metadata bytes are checked only after all semantic checks. Success stores canonical demands sorted by subject then dimension and a deep copy of metadata.

`Replace(id, demands, metadata)` has the same structural rules, requires the ID to exist, and atomically removes its old demands before validating and applying the replacement. This permits moving capacity between dimensions. `Release(id)` removes an existing reservation. Missing IDs return `ErrNotFound`; duplicate creation returns `ErrExists`; unknown dimensions return `ErrNotFound`; quota or arithmetic failures return `ErrExceeded`; global count or metadata capacity failures return `ErrCapacity`. Each success increments generation once, while every failure preserves the exact prior state.

## Subject deletion and snapshot

`DeleteSubject(subject)` returns `ErrNotFound` when absent and `ErrBusy` if any active reservation references it. Otherwise it deletes the subject and increments generation once.

`Snapshot` reports generation, subject/reservation counts, total metadata bytes, subjects sorted by name with dimensions sorted by name, and reservations sorted by ID with canonical demands. Usage is derived from active reservations. All metadata byte slices are deep copies independent from inputs, internal state, and other returns.

All public methods are safe for concurrent use. A single mutex and maps are acceptable. Reserve/Replace are expected O(demands log demands), snapshots O(n log n), and space O(subject dimensions + reservation demands + metadata bytes).
