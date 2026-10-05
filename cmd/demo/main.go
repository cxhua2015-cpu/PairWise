package main

import (
	"example.com/pairwise/readyqueue265/readyqueue265"
	"fmt"
)

func main() {
	q, _ := readyqueue265.New(readyqueue265.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue265.Batch{Ops: []readyqueue265.Op{{Kind: readyqueue265.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
