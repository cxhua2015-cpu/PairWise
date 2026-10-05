package main

import (
	"example.com/pairwise/taskqueue105/taskqueue105"
	"fmt"
)

func main() {
	q, _ := taskqueue105.New(taskqueue105.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(taskqueue105.Batch{Ops: []taskqueue105.Op{{Kind: taskqueue105.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
