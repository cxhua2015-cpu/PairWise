package main

import (
	"example.com/pairwise/readyqueue215/readyqueue215"
	"fmt"
)

func main() {
	q, _ := readyqueue215.New(readyqueue215.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue215.Batch{Ops: []readyqueue215.Op{{Kind: readyqueue215.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
