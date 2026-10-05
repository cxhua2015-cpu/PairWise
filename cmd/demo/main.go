package main

import (
	"example.com/pairwise/migrationqueue/migrationqueue"
	"fmt"
)

func main() {
	q, _ := migrationqueue.New(migrationqueue.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(migrationqueue.Batch{Ops: []migrationqueue.Op{{Kind: migrationqueue.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
