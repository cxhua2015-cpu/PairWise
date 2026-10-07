package main

import (
	"example.com/pairwise/readyqueue415/readyqueue415"
	"fmt"
)

func main() {
	q, _ := readyqueue415.New(readyqueue415.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue415.Batch{Ops: []readyqueue415.Op{{Kind: readyqueue415.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
