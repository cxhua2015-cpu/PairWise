package main

import (
	"example.com/pairwise/readyqueue325/readyqueue325"
	"fmt"
)

func main() {
	q, _ := readyqueue325.New(readyqueue325.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue325.Batch{Ops: []readyqueue325.Op{{Kind: readyqueue325.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
