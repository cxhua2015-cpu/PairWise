package main

import (
	"example.com/pairwise/taskqueue180/taskqueue180"
	"fmt"
)

func main() {
	q, _ := taskqueue180.New(taskqueue180.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(taskqueue180.Batch{Ops: []taskqueue180.Op{{Kind: taskqueue180.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
