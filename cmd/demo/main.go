package main

import (
	"example.com/pairwise/delayedqueue/delayedqueue"
	"fmt"
)

func main() {
	q, _ := delayedqueue.New(delayedqueue.Options{MaxJobs: 8, MaxIDBytes: 16, MaxPayloadBytes: 16, MaxTotalPayloadBytes: 64})
	_, _ = q.Apply(delayedqueue.Batch{Now: 1, Ops: []delayedqueue.Op{{Kind: delayedqueue.Enqueue, ID: "slow", Priority: 1, ReadyAt: 5, Payload: []byte("s")}, {Kind: delayedqueue.Enqueue, ID: "fast", Priority: 9, ReadyAt: 2, Payload: []byte("f")}}})
	x, _ := q.Take(2, 4)
	s := q.Snapshot()
	fmt.Printf("taken=%d first=%s remaining=%d revision=%d now=%d\n", len(x), x[0].ID, len(s.Jobs), s.NextRevision-1, s.Now)
}
