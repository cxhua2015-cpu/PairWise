package main

import (
	"example.com/pairwise/taskqueue110/taskqueue110"
	"fmt"
)

func main() {
	q, _ := taskqueue110.New(taskqueue110.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(taskqueue110.Batch{Ops: []taskqueue110.Op{{Kind: taskqueue110.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
