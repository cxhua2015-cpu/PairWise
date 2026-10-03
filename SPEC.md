# Concurrent JSON Patch Document Store

## 1. General

`jsondoc` stores one in-memory JSON document and applies RFC 6902-style patch batches. All exported methods must be safe for concurrent calls. Only the Go standard library may be used. Exported declarations in the skeleton, including sentinel errors, are public API and must remain present with unchanged signatures.

## 2. Construction, JSON and ownership

`New` requires `MaxNodes > 0`, otherwise `ErrInvalidOptions`. Initial JSON must contain exactly one valid JSON value; malformed input or trailing non-whitespace returns `ErrInvalidJSON`. Decode numbers with `json.Decoder.UseNumber`.

The initial revision is 1. A JSON node is each object, array, string, number, boolean or null value; object member names are not nodes. The initial document must not exceed `MaxNodes`, otherwise `ErrCapacity`.

Inputs must be copied before successful return. `Result.Document` and `Snapshot.Document` are compact JSON with deterministic object-key ordering and are isolated from input, internal state and other returns. Number lexemes may be preserved.

## 3. Operations and validation

Supported lower-case operation names are `add`, `remove`, `replace`, `move`, `copy`, and `test`. Any other spelling returns `ErrInvalidOperation`.

Every operation is structurally validated before revision comparison or execution:

- `add`, `replace`, and `test` require exactly one valid JSON value in `Value` and require empty `From`.
- `remove` requires `Value == nil` and empty `From`.
- `move` and `copy` require `Value == nil`; `From` must be a valid pointer, and the empty string denotes the root.
- `Path` and applicable `From` must be valid JSON Pointers.

An empty but non-nil `Value` is invalid JSON. Structural errors are returned as `*OpError` with the zero-based operation index, the operation path, and an unwrap-compatible sentinel cause.

## 4. JSON Pointer

The empty string points to the document root. Other pointers must start with `/`. Tokens decode `~1` to `/` and `~0` to `~`; every other `~` escape is invalid. Object tokens may be empty.

When traversing arrays, indices are decimal `0` or a non-zero digit followed by digits. Leading zeros, signs, overflow and `-` are invalid except that the final token of `add` may be `-` to append. Access indices must be less than length; an add insertion index may equal length. Errors use `ErrInvalidPointer`, `ErrInvalidIndex`, `ErrNotFound`, or `ErrTypeMismatch` as appropriate.

## 5. Operation semantics

- `add`: at root, replace the whole document; in an object, create or replace a member; in an array, insert before the index or append with `-`.
- `remove`: remove an existing object member or array element. Removing the root returns `ErrRootRemoval`.
- `replace`: replace an existing value; root replacement is allowed.
- `copy`: deep-copy the value at `From`, then apply add semantics at `Path`.
- `move`: reject moving a location into its own strict descendant with `ErrMoveIntoChild`. When `From == Path`, it is a successful no-op. Otherwise remove `From`, then apply add to `Path` against the post-removal document. Moving the root into a non-root location is a move into its descendant and is rejected; moving a child to the root is allowed.
- `test`: compare the target and supplied value using JSON semantic equality. Object member order and number lexemes do not matter. JSON numbers compare by exact mathematical value, so `1`, `1.0`, and `1e0` are equal. A mismatch returns `ErrTestFailed`.

All execution failures are wrapped in `*OpError` at the failing operation.

## 6. Atomicity, revision and capacity

`Apply(expectedRevision, ops)` structurally validates every operation first. It then requires `expectedRevision == currentRevision`, otherwise `ErrRevisionConflict` (not wrapped). Operations execute in input order on an isolated deep copy, so later operations see earlier results.

Any error returns a zero `Result` and leaves document and revision unchanged. Node capacity is checked only after every operation has executed, allowing temporary overflow that is removed later in the same batch. Final overflow returns `ErrCapacity` and rolls back.

A successful batch containing at least one `add`, `remove`, `replace`, `move`, or `copy` advances revision exactly once, even if the final JSON is semantically unchanged. An empty or test-only successful batch leaves revision unchanged.

## 7. Snapshot and errors

`Snapshot` returns the current revision, node count and document. `OpError.Error` must include the operation index and wrapped error text; `Unwrap` returns its cause so `errors.Is` works.

Structural validation precedes revision conflict checks. Execution follows operation order. Capacity is checked last.
