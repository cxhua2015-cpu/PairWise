package main

import (
	"example.com/pairwise/taskqueue135/taskqueue135"
	"fmt"
)

func main() {
	q, _ := taskqueue135.New(taskqueue135.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(taskqueue135.Batch{Ops: []taskqueue135.Op{{Kind: taskqueue135.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
