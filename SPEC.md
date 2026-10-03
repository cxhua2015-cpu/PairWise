# MVCC Watch Store Specification

## 1. Overview

`mvcc` is an in-memory multi-version key/value store. All exported methods must be safe for concurrent calls. The implementation may serialize calls with one mutex. It must use only the Go standard library.

Declarations in the supplied skeleton are public API. They must not be removed or have their signatures changed, including exported sentinel errors.

## 2. Options and construction

`New(Options)` returns `ErrInvalidOptions` when `MaxLiveBytes <= 0`. `MaxLiveBytes` counts the byte lengths of keys and values of currently live keys only. Historical versions, tombstones and events do not count toward this limit.

A new store has revision 0 and compact revision 0.

## 3. Keys, ranges and ownership

Keys must be non-empty. An exact operation uses `End == ""`. A range uses the half-open interval `[Key, End)` and requires `End > Key` in bytewise string order. Invalid keys return `ErrInvalidKey`; invalid intervals return `ErrInvalidRange`.

Keys are ordered by bytewise Go string order. Returned range results use that order. All input `[]byte` values must be copied before a successful method returns. Every returned `[]byte`, `KV`, `Event`, `OpResponse`, and `Snapshot` must be isolated from internal state and from later returned values.

## 4. Revisions and versions

Every successful call containing at least one effective write allocates exactly `currentRevision + 1`. All effective writes in one transaction share that revision. Events for those writes receive consecutive `Sequence` values beginning at 0 in operation order.

`Put` is always an effective write, including replacement with identical bytes. A key's `CreateRevision` is set when its current lifetime starts, `ModRevision` is the write revision, and `Version` starts at 1 and increments on each put in the same lifetime. A delete ends the lifetime. Recreating the key starts with a new create revision and version 1.

Deleting an absent key is a successful no-op: it allocates no revision, returns `deleted=false`, and returns the zero `Event`.

## 5. Reads

`Range(Key, End, Revision)` returns live keys at the requested revision. Revision 0 means the current revision. A non-zero revision greater than the current revision returns `ErrFutureRevision`. A non-zero revision less than or equal to the compact revision returns `ErrCompacted`. Exact lookup returns either zero or one KV.

## 6. Events and watch replay

Each effective put or delete produces one event. Put events contain the new live KV. Delete events contain a tombstone KV with the deleted key, nil value, the deleted lifetime's create revision, the delete revision as mod revision, and the prior version. `Prev` is nil when a put creates an absent key; otherwise it is the live value immediately before the write. Events are ordered by `(Revision, Sequence)`.

`Watch(Prefix, AfterRevision, Limit)` returns retained events whose revision is strictly greater than `AfterRevision` and whose key starts with `Prefix`. Empty prefix is allowed. Negative limit returns `ErrInvalidLimit`; limit 0 means unlimited. `AfterRevision > currentRevision` returns `ErrFutureRevision`. `AfterRevision < compactRevision` returns `ErrCompacted`; equality is valid.

## 7. Transactions

`Txn` first validates all compares and every operation in both branches. Any validation error returns without evaluating conditions or mutating state. Duplicate compares and writes are allowed.

Compares are evaluated against one snapshot at the current revision. For an absent key: exists is false, value is nil, mod revision and version are 0. `CompareValue` uses `bytes.Compare`; numeric targets use integer comparison. Only Equal and NotEqual are valid for `CompareExists`.

The success branch is selected only when every compare is true; otherwise the failure branch is selected. Selected operations execute in input order on an isolated candidate state. A branch range operation must have `Revision == 0` and observes all earlier writes in that branch. Put and delete operations require `Revision == 0`; put requires exact key and delete may be exact or ranged.

All effective branch writes share one new revision. Their events have consecutive sequences in effective-write order; no-op deletes produce no event. `TxnResponse.Responses` has one entry per selected operation. Put responses contain the resulting KV. Delete responses contain `Deleted`; range responses contain sorted KVs.

Capacity is checked only after the whole selected branch executes. This permits a branch to exceed capacity temporarily and later delete enough data. If final live bytes exceed `MaxLiveBytes`, return `ErrCapacity` with a zero response and leave revision, data, history and events unchanged. Any execution error has the same rollback guarantee.

A read-only or entirely no-op transaction returns the existing current revision and produces no events.

## 8. Compaction

`Compact(Revision)` requires `Revision > 0`, `Revision <= currentRevision`, and `Revision >= compactRevision`; otherwise it returns `ErrInvalidRevision` or `ErrFutureRevision` as applicable. Repeating the current compact revision is successful.

Compaction removes replay events at revisions `<= Revision`. For each key it retains the newest version at or before the compact revision as a base plus every later version. This is sufficient for reads after the compact point. Reads at revisions `<= compactRevision` remain forbidden even when base data is retained.

## 9. Snapshot

`Snapshot` reports current revision, compact revision, current live byte count, and all current live keys in bytewise key order. It never exposes tombstones.

## 10. Error precedence

Methods validate structural arguments first. Revision/compaction checks come next. Transaction capacity is checked last against the completed candidate. On every error, state is unchanged.
