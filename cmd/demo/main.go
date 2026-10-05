package main

import (
	"example.com/pairwise/taskqueue085/taskqueue085"
	"fmt"
)

func main() {
	q, _ := taskqueue085.New(taskqueue085.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(taskqueue085.Batch{Ops: []taskqueue085.Op{{Kind: taskqueue085.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
