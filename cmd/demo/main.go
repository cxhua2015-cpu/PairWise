package main

import (
	"example.com/pairwise/taskqueue140/taskqueue140"
	"fmt"
)

func main() {
	q, _ := taskqueue140.New(taskqueue140.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(taskqueue140.Batch{Ops: []taskqueue140.Op{{Kind: taskqueue140.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
