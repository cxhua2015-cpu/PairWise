package main

import (
	"example.com/pairwise/taskqueue155/taskqueue155"
	"fmt"
)

func main() {
	q, _ := taskqueue155.New(taskqueue155.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(taskqueue155.Batch{Ops: []taskqueue155.Op{{Kind: taskqueue155.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
