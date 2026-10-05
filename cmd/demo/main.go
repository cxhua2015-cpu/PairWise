package main

import (
	"example.com/pairwise/readyqueue280/readyqueue280"
	"fmt"
)

func main() {
	q, _ := readyqueue280.New(readyqueue280.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue280.Batch{Ops: []readyqueue280.Op{{Kind: readyqueue280.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
