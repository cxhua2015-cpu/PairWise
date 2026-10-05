package main

import (
	"example.com/pairwise/readyqueue290/readyqueue290"
	"fmt"
)

func main() {
	q, _ := readyqueue290.New(readyqueue290.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue290.Batch{Ops: []readyqueue290.Op{{Kind: readyqueue290.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
