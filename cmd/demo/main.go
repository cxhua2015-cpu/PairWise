package main

import (
	"example.com/pairwise/readyqueue205/readyqueue205"
	"fmt"
)

func main() {
	q, _ := readyqueue205.New(readyqueue205.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue205.Batch{Ops: []readyqueue205.Op{{Kind: readyqueue205.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
