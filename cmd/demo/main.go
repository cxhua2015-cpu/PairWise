package main

import (
	"example.com/pairwise/taskqueue145/taskqueue145"
	"fmt"
)

func main() {
	q, _ := taskqueue145.New(taskqueue145.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(taskqueue145.Batch{Ops: []taskqueue145.Op{{Kind: taskqueue145.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
