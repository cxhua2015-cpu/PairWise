package main

import (
	"example.com/pairwise/readyqueue320/readyqueue320"
	"fmt"
)

func main() {
	q, _ := readyqueue320.New(readyqueue320.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue320.Batch{Ops: []readyqueue320.Op{{Kind: readyqueue320.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
