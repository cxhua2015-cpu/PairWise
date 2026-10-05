package main

import (
	"example.com/pairwise/readyqueue405/readyqueue405"
	"fmt"
)

func main() {
	q, _ := readyqueue405.New(readyqueue405.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue405.Batch{Ops: []readyqueue405.Op{{Kind: readyqueue405.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
