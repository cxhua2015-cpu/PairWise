package main

import (
	"example.com/pairwise/readyqueue245/readyqueue245"
	"fmt"
)

func main() {
	q, _ := readyqueue245.New(readyqueue245.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue245.Batch{Ops: []readyqueue245.Op{{Kind: readyqueue245.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
