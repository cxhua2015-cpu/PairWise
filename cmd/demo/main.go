package main

import (
	"example.com/pairwise/readyqueue275/readyqueue275"
	"fmt"
)

func main() {
	q, _ := readyqueue275.New(readyqueue275.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue275.Batch{Ops: []readyqueue275.Op{{Kind: readyqueue275.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
