package main

import (
	"example.com/pairwise/readyqueue495/readyqueue495"
	"fmt"
)

func main() {
	q, _ := readyqueue495.New(readyqueue495.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue495.Batch{Ops: []readyqueue495.Op{{Kind: readyqueue495.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
