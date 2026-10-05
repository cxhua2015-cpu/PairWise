package main

import (
	"example.com/pairwise/readyqueue225/readyqueue225"
	"fmt"
)

func main() {
	q, _ := readyqueue225.New(readyqueue225.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue225.Batch{Ops: []readyqueue225.Op{{Kind: readyqueue225.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
