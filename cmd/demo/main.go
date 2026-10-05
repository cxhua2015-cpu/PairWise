package main

import (
	"example.com/pairwise/taskqueue100/taskqueue100"
	"fmt"
)

func main() {
	q, _ := taskqueue100.New(taskqueue100.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(taskqueue100.Batch{Ops: []taskqueue100.Op{{Kind: taskqueue100.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
