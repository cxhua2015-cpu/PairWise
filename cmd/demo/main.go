package main

import (
	"example.com/pairwise/readyqueue295/readyqueue295"
	"fmt"
)

func main() {
	q, _ := readyqueue295.New(readyqueue295.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue295.Batch{Ops: []readyqueue295.Op{{Kind: readyqueue295.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
