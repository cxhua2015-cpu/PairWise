package main

import (
	"example.com/pairwise/readyqueue365/readyqueue365"
	"fmt"
)

func main() {
	q, _ := readyqueue365.New(readyqueue365.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue365.Batch{Ops: []readyqueue365.Op{{Kind: readyqueue365.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
