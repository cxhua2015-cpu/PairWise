package main

import (
	"example.com/pairwise/taskqueue125/taskqueue125"
	"fmt"
)

func main() {
	q, _ := taskqueue125.New(taskqueue125.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(taskqueue125.Batch{Ops: []taskqueue125.Op{{Kind: taskqueue125.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
