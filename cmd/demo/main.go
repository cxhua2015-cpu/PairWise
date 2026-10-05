package main

import (
	"example.com/pairwise/readyqueue400/readyqueue400"
	"fmt"
)

func main() {
	q, _ := readyqueue400.New(readyqueue400.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue400.Batch{Ops: []readyqueue400.Op{{Kind: readyqueue400.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
