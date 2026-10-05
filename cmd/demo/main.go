package main

import (
	"example.com/pairwise/readyqueue250/readyqueue250"
	"fmt"
)

func main() {
	q, _ := readyqueue250.New(readyqueue250.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue250.Batch{Ops: []readyqueue250.Op{{Kind: readyqueue250.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
