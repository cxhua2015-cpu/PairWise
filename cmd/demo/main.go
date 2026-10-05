package main

import (
	"example.com/pairwise/prioritybox/prioritybox"
	"fmt"
)

func main() {
	q, _ := prioritybox.New(prioritybox.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(prioritybox.Batch{Ops: []prioritybox.Op{{Kind: prioritybox.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
