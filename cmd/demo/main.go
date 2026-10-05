package main

import (
	"example.com/pairwise/readyqueue255/readyqueue255"
	"fmt"
)

func main() {
	q, _ := readyqueue255.New(readyqueue255.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue255.Batch{Ops: []readyqueue255.Op{{Kind: readyqueue255.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
