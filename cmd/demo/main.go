package main

import (
	"example.com/pairwise/readyqueue425/readyqueue425"
	"fmt"
)

func main() {
	q, _ := readyqueue425.New(readyqueue425.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue425.Batch{Ops: []readyqueue425.Op{{Kind: readyqueue425.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
