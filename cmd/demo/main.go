package main

import (
	"example.com/pairwise/readyqueue410/readyqueue410"
	"fmt"
)

func main() {
	q, _ := readyqueue410.New(readyqueue410.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue410.Batch{Ops: []readyqueue410.Op{{Kind: readyqueue410.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
