package main

import (
	"example.com/pairwise/taskqueue190/taskqueue190"
	"fmt"
)

func main() {
	q, _ := taskqueue190.New(taskqueue190.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(taskqueue190.Batch{Ops: []taskqueue190.Op{{Kind: taskqueue190.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
