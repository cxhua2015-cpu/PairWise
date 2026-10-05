package main

import (
	"example.com/pairwise/readyqueue260/readyqueue260"
	"fmt"
)

func main() {
	q, _ := readyqueue260.New(readyqueue260.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue260.Batch{Ops: []readyqueue260.Op{{Kind: readyqueue260.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
