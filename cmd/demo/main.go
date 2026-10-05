package main

import (
	"example.com/pairwise/readyqueue375/readyqueue375"
	"fmt"
)

func main() {
	q, _ := readyqueue375.New(readyqueue375.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue375.Batch{Ops: []readyqueue375.Op{{Kind: readyqueue375.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
