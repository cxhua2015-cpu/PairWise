package main

import (
	"example.com/pairwise/readyqueue230/readyqueue230"
	"fmt"
)

func main() {
	q, _ := readyqueue230.New(readyqueue230.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue230.Batch{Ops: []readyqueue230.Op{{Kind: readyqueue230.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
