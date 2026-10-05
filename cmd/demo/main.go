package main

import (
	"example.com/pairwise/readyqueue220/readyqueue220"
	"fmt"
)

func main() {
	q, _ := readyqueue220.New(readyqueue220.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue220.Batch{Ops: []readyqueue220.Op{{Kind: readyqueue220.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
