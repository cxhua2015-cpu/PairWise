package main

import (
	"example.com/pairwise/taskqueue200/taskqueue200"
	"fmt"
)

func main() {
	q, _ := taskqueue200.New(taskqueue200.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(taskqueue200.Batch{Ops: []taskqueue200.Op{{Kind: taskqueue200.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
