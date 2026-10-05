package main

import (
	"example.com/pairwise/readyqueue315/readyqueue315"
	"fmt"
)

func main() {
	q, _ := readyqueue315.New(readyqueue315.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue315.Batch{Ops: []readyqueue315.Op{{Kind: readyqueue315.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
