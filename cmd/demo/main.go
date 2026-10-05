package main

import (
	"example.com/pairwise/readyqueue435/readyqueue435"
	"fmt"
)

func main() {
	q, _ := readyqueue435.New(readyqueue435.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue435.Batch{Ops: []readyqueue435.Op{{Kind: readyqueue435.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
