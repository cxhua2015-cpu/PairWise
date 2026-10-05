package main

import (
	"example.com/pairwise/readyqueue360/readyqueue360"
	"fmt"
)

func main() {
	q, _ := readyqueue360.New(readyqueue360.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue360.Batch{Ops: []readyqueue360.Op{{Kind: readyqueue360.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
