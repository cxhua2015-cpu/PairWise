package main

import (
	"example.com/pairwise/readyqueue270/readyqueue270"
	"fmt"
)

func main() {
	q, _ := readyqueue270.New(readyqueue270.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue270.Batch{Ops: []readyqueue270.Op{{Kind: readyqueue270.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
