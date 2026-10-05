package main

import (
	"example.com/pairwise/readyqueue445/readyqueue445"
	"fmt"
)

func main() {
	q, _ := readyqueue445.New(readyqueue445.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue445.Batch{Ops: []readyqueue445.Op{{Kind: readyqueue445.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
