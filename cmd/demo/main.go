package main

import (
	"example.com/pairwise/notificationqueue/notificationqueue"
	"fmt"
)

func main() {
	q, _ := notificationqueue.New(notificationqueue.Options{MaxItems: 4, MaxIDBytes: 8})
	x, _ := q.Apply(notificationqueue.Batch{Ops: []notificationqueue.Op{{Kind: notificationqueue.Enqueue, ID: "a", Priority: 2}}})
	p, _ := q.Pop(0, 1)
	fmt.Printf("generation=%d revision=%d popped=%s remaining=%d\n", x.Generation, x.Revision, p[0].ID, len(q.Snapshot().Items))
}
