package main

import (
	"example.com/pairwise/readyqueue310/readyqueue310"
	"fmt"
)

func main() {
	q, _ := readyqueue310.New(readyqueue310.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue310.Batch{Ops: []readyqueue310.Op{{Kind: readyqueue310.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
