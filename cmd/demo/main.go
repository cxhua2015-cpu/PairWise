package main

import (
	"example.com/pairwise/taskqueue195/taskqueue195"
	"fmt"
)

func main() {
	q, _ := taskqueue195.New(taskqueue195.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(taskqueue195.Batch{Ops: []taskqueue195.Op{{Kind: taskqueue195.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
