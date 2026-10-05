package main

import (
	"example.com/pairwise/retrybox/retrybox"
	"fmt"
)

func main() {
	q, _ := retrybox.New(retrybox.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(retrybox.Batch{Ops: []retrybox.Op{{Kind: retrybox.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
