package main

import (
	"example.com/pairwise/readyqueue340/readyqueue340"
	"fmt"
)

func main() {
	q, _ := readyqueue340.New(readyqueue340.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue340.Batch{Ops: []readyqueue340.Op{{Kind: readyqueue340.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
