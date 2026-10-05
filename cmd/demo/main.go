package main

import (
	"example.com/pairwise/taskqueue130/taskqueue130"
	"fmt"
)

func main() {
	q, _ := taskqueue130.New(taskqueue130.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(taskqueue130.Batch{Ops: []taskqueue130.Op{{Kind: taskqueue130.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
