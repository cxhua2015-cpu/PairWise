package main

import (
	"example.com/pairwise/taskqueue170/taskqueue170"
	"fmt"
)

func main() {
	q, _ := taskqueue170.New(taskqueue170.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(taskqueue170.Batch{Ops: []taskqueue170.Op{{Kind: taskqueue170.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
