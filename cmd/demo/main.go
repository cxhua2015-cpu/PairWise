package main

import (
	"example.com/pairwise/readyqueue455/readyqueue455"
	"fmt"
)

func main() {
	q, _ := readyqueue455.New(readyqueue455.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue455.Batch{Ops: []readyqueue455.Op{{Kind: readyqueue455.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
