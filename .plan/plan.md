# Plan: metacatalog216

1. Implement Store: sync.RWMutex, map index, generation, nextRevision.
2. Apply: full structural validation -> candidate transaction (overlay shadow state) -> execute in order -> final capacity checks -> commit or rollback.
3. Get/Snapshot deep-copy values; Snapshot sorted by name.
4. Add boundary/concurrency tests in new _test.go file.
5. Update README (index, candidate txn, ownership, complexity).
6. Run go test ./..., go test -race ./..., go run ./cmd/demo.
