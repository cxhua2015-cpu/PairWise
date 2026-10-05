package main

import (
	"example.com/pairwise/deliveryqueue/deliveryqueue"
	"fmt"
)

func main() {
	q, _ := deliveryqueue.New(deliveryqueue.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(deliveryqueue.Batch{Ops: []deliveryqueue.Op{{Kind: deliveryqueue.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
