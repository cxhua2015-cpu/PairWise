package main

import (
	"example.com/pairwise/readyqueue240/readyqueue240"
	"fmt"
)

func main() {
	q, _ := readyqueue240.New(readyqueue240.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue240.Batch{Ops: []readyqueue240.Op{{Kind: readyqueue240.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
