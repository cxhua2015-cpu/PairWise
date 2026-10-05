package main

import (
	"example.com/pairwise/taskqueue115/taskqueue115"
	"fmt"
)

func main() {
	q, _ := taskqueue115.New(taskqueue115.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(taskqueue115.Batch{Ops: []taskqueue115.Op{{Kind: taskqueue115.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
