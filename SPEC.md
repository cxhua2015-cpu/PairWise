# CAS store specification

`New` requires positive `MaxKeys`, `MaxValueBytes`, and `MaxNameBytes`. Keys are nonempty, at most `MaxNameBytes` bytes, and contain only ASCII letters, digits, dot, underscore, slash, or hyphen. Values for Put and Value comparisons must be non-nil; empty is valid. A single value may be at most `MaxValueBytes` bytes.

Compare kinds are Exists, NotExists, Revision, and Value. Exists/NotExists require Revision zero and Value nil. Revision requires a positive Revision and Value nil. Value requires Revision zero and non-nil Value. Write kinds are Put and Delete. Put requires a valid key and non-nil Value; Delete requires a valid key and nil Value. Every compare is structurally validated in order, then every write is structurally validated in order, before reading state. Unknown kinds or malformed fields return `ErrInvalidInput`.

All compares evaluate against the transaction's initial snapshot. Exists and NotExists test presence; Revision tests exact entry revision; Value compares bytes. A false compare returns `Succeeded:false` with no error or state change. If all comparisons pass, writes execute sequentially on an isolated candidate. Put creates or replaces a key and receives the current `NextRevision`, starting at 1, then increments the candidate counter. Delete of a missing key returns `ErrNotFound`. Repeated writes are allowed. Final key count and total live value bytes are checked only after all writes. Any failure rolls back state and revision allocation.

A successful transaction with at least one write increments generation once. A successful read-only transaction does not. `TxnResult.Revision` is the latest allocated revision, or zero before any Put.

`Get` validates a key and returns a deep copy. `List(prefix,after,limit)` accepts an empty prefix and after; nonempty values must be structurally valid, limit is 1..1000. It returns entries whose keys start with prefix and are lexicographically greater than after, ordered by key. `Snapshot` reports generation, NextRevision, key count, value bytes, and entries by key. All returned and input Value slices are ownership-isolated. All methods are concurrency-safe.
