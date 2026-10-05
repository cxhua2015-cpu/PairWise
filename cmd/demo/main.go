package main

import (
	"example.com/pairwise/readyqueue305/readyqueue305"
	"fmt"
)

func main() {
	q, _ := readyqueue305.New(readyqueue305.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue305.Batch{Ops: []readyqueue305.Op{{Kind: readyqueue305.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
