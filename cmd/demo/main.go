package main

import (
	"example.com/pairwise/readyqueue480/readyqueue480"
	"fmt"
)

func main() {
	q, _ := readyqueue480.New(readyqueue480.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue480.Batch{Ops: []readyqueue480.Op{{Kind: readyqueue480.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
