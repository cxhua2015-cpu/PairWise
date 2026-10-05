package main

import (
	"example.com/pairwise/taskqueue160/taskqueue160"
	"fmt"
)

func main() {
	q, _ := taskqueue160.New(taskqueue160.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(taskqueue160.Batch{Ops: []taskqueue160.Op{{Kind: taskqueue160.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
