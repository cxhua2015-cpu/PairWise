package main

import (
	"example.com/pairwise/readyqueue490/readyqueue490"
	"fmt"
)

func main() {
	q, _ := readyqueue490.New(readyqueue490.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue490.Batch{Ops: []readyqueue490.Op{{Kind: readyqueue490.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
