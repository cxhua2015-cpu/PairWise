package main

import (
	"example.com/pairwise/dispatchbox/dispatchbox"
	"fmt"
)

func main() {
	q, _ := dispatchbox.New(dispatchbox.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(dispatchbox.Batch{Ops: []dispatchbox.Op{{Kind: dispatchbox.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
