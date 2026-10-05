package main

import (
	"example.com/pairwise/taskqueue185/taskqueue185"
	"fmt"
)

func main() {
	q, _ := taskqueue185.New(taskqueue185.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(taskqueue185.Batch{Ops: []taskqueue185.Op{{Kind: taskqueue185.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
