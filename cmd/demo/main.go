package main

import (
	"example.com/pairwise/readyqueue350/readyqueue350"
	"fmt"
)

func main() {
	q, _ := readyqueue350.New(readyqueue350.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue350.Batch{Ops: []readyqueue350.Op{{Kind: readyqueue350.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
