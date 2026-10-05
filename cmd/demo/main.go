package main

import (
	"example.com/pairwise/readyqueue235/readyqueue235"
	"fmt"
)

func main() {
	q, _ := readyqueue235.New(readyqueue235.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue235.Batch{Ops: []readyqueue235.Op{{Kind: readyqueue235.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
