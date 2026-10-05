package main

import (
	"example.com/pairwise/readyqueue500/readyqueue500"
	"fmt"
)

func main() {
	q, _ := readyqueue500.New(readyqueue500.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue500.Batch{Ops: []readyqueue500.Op{{Kind: readyqueue500.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
