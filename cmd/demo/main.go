package main

import (
	"example.com/pairwise/settlementqueue/settlementqueue"
	"fmt"
)

func main() {
	q, _ := settlementqueue.New(settlementqueue.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(settlementqueue.Batch{Ops: []settlementqueue.Op{{Kind: settlementqueue.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
