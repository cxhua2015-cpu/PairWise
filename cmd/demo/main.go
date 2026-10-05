package main

import (
	"example.com/pairwise/readyqueue485/readyqueue485"
	"fmt"
)

func main() {
	q, _ := readyqueue485.New(readyqueue485.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue485.Batch{Ops: []readyqueue485.Op{{Kind: readyqueue485.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
