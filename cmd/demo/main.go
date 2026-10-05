package main

import (
	"example.com/pairwise/readyqueue330/readyqueue330"
	"fmt"
)

func main() {
	q, _ := readyqueue330.New(readyqueue330.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue330.Batch{Ops: []readyqueue330.Op{{Kind: readyqueue330.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
