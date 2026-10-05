package main

import (
	"example.com/pairwise/readyqueue475/readyqueue475"
	"fmt"
)

func main() {
	q, _ := readyqueue475.New(readyqueue475.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(readyqueue475.Batch{Ops: []readyqueue475.Op{{Kind: readyqueue475.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
