package main

import (
	"example.com/pairwise/readyqueue390/readyqueue390"
	"fmt"
)

func main() {
	q, _ := readyqueue390.New(readyqueue390.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue390.Batch{Ops: []readyqueue390.Op{{Kind: readyqueue390.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
