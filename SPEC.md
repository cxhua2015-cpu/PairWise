# Dynamic Path Router Specification

## 1. Public contract

`router` is an in-memory dynamic request router. Every exported method must be safe for concurrent calls. Only the Go standard library may be used. All declarations in the supplied skeleton are public API and must remain present with unchanged signatures, including exported sentinel errors.

## 2. Construction and ownership

`New` returns `ErrInvalidOptions` unless `MaxRoutes > 0`. Route `Value` bytes are copied on successful insertion. All returned values, parameters, route values and snapshots are deep copies isolated from inputs, internal state and other returns.

## 3. Methods, names and patterns

Methods must be non-empty uppercase HTTP tokens. Lowercase, spaces, separators and method `HEAD` are rejected with `ErrInvalidMethod`; HEAD is provided only through GET fallback. Route names are non-empty and globally unique.

A pattern must start with `/`. `/` is the root pattern. Other patterns contain non-empty slash-separated segments and may not end in `/`.

- A static segment is a valid URL-escaped segment. It is decoded during registration and may not decode to empty, `.`、`..`, or contain `/`.
- `:name` is a single-segment parameter.
- `*name` is a catch-all parameter, must be the final segment, and matches one or more path segments.
- Parameter names are non-empty ASCII identifiers `[A-Za-z_][A-Za-z0-9_]*` and may not repeat in one pattern.

Malformed patterns return `ErrInvalidPattern`. Under the same method, patterns with the same structural shape conflict even when parameter names differ. For example `/users/:id` conflicts with `/users/:name`. Route names also conflict globally. Both return `ErrConflict`.

## 4. Changes and atomic batches

`ChangeAdd` inserts `Change.Route`; all other fields are ignored. `ChangeRemove` removes the route named by `Change.Name`; missing names return `ErrNotFound`. Other change types return `ErrInvalidChange`.

`Apply` has the same semantics as a one-element `ApplyBatch`. `ApplyBatch` validates and executes changes in input order against an isolated candidate. A later change may remove a route added earlier in the same batch or free a name/shape for later reuse. Any error rolls back the entire batch and returns a zero `UpdateResult`.

`MaxRoutes` is checked only against the final candidate, so a batch may temporarily exceed it. Final overflow returns `ErrCapacity` and rolls back. A successful non-empty batch increments generation exactly once. An empty batch succeeds without changing generation.

## 5. Request path validation and matching

`Match` requires a valid uppercase method other than HEAD restrictions below: request method `HEAD` is accepted, but routes cannot be registered for HEAD. The escaped path must start with `/`; `/` is valid; other paths may not end in `/` or contain empty segments. Every segment is decoded with `url.PathUnescape`; malformed escapes or a decoded slash, empty, `.` or `..` return `ErrInvalidPath`.

Candidates are ranked lexicographically by segment kind from left to right: static beats parameter, parameter beats catch-all. If the common prefix has equal kinds, the route with more non-catch-all segments wins. Registration order never affects selection.

An exact method match is tried first. For request method HEAD, GET is used as a fallback and `HeadFallback` is true. When no selected-method route matches but the path matches routes of other methods, return `MethodNotAllowed=true` and `Allowed` as sorted unique registered methods; add `HEAD` whenever GET is allowed. In this case `Found` is false. When no route matches any method, both flags are false.

Parameters are returned in pattern order. Single parameters contain one decoded segment. Catch-all values join decoded remaining segments with `/`.

## 6. Reverse construction

`Build(name, params)` finds a route by its global name or returns `ErrNotFound`. It requires exactly the route's parameter names: missing or extra keys return `ErrInvalidParams`. Single-parameter values must be non-empty and contain no `/`, `.` or `..`. Catch-all values must contain one or more slash-separated valid segments. Every segment is escaped with `url.PathEscape`. The returned path therefore round-trips through `Match` without changing decoded parameter values.

## 7. Snapshot and ordering

`Snapshot` returns current generation and all registered routes sorted by route name. Match `Allowed` is sorted lexicographically. Batch results contain the new generation and final route count.

## 8. Error precedence

Structural validation precedes conflict/not-found checks. Batch capacity is checked last. Every error leaves generation and routes unchanged.
