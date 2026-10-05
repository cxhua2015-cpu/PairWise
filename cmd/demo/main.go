package main

import (
	"example.com/pairwise/auctionqueue/auctionqueue"
	"fmt"
)

func main() {
	q, _ := auctionqueue.New(auctionqueue.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(auctionqueue.Batch{Ops: []auctionqueue.Op{{Kind: auctionqueue.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
