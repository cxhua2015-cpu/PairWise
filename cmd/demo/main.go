package main

import (
	"example.com/pairwise/readyqueue460/readyqueue460"
	"fmt"
)

func main() {
	q, _ := readyqueue460.New(readyqueue460.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue460.Batch{Ops: []readyqueue460.Op{{Kind: readyqueue460.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
