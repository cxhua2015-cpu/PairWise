package main

import (
	"example.com/pairwise/readyqueue345/readyqueue345"
	"fmt"
)

func main() {
	q, _ := readyqueue345.New(readyqueue345.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue345.Batch{Ops: []readyqueue345.Op{{Kind: readyqueue345.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
