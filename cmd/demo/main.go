package main

import (
	"fmt"
	"log"

	"example.com/pairwise/fairqueue/fairqueue"
)

func main() {
	q, err := fairqueue.New(fairqueue.Options{MaxTasks: 8, MaxPayloadBytes: 64, MaxNameBytes: 16}, []fairqueue.QueueWeight{{Queue: "bulk", Weight: 1}, {Queue: "urgent", Weight: 2}})
	if err != nil {
		log.Fatal(err)
	}
	_, err = q.ApplyBatch([]fairqueue.Change{{Kind: fairqueue.Put, ID: "u1", Queue: "urgent", Payload: []byte("one")}, {Kind: fairqueue.Put, ID: "u2", Queue: "urgent", Payload: []byte("two")}, {Kind: fairqueue.Put, ID: "b1", Queue: "bulk", Payload: []byte("three")}})
	if err != nil {
		log.Fatal(err)
	}
	r, err := q.Dequeue(3)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("order=%s,%s,%s generation=%d cursor=%d remaining=%d\n", r.Tasks[0].ID, r.Tasks[1].ID, r.Tasks[2].ID, r.Generation, r.Cursor, q.Snapshot().Tasks)
}
