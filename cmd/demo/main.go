package main

import (
	"example.com/pairwise/deadlinequeue/deadlinequeue"
	"fmt"
	"log"
)

func main() {
	q, err := deadlinequeue.New(deadlinequeue.Options{MaxTasks: 8, MaxPayloadBytes: 64, MaxNameBytes: 32})
	if err != nil {
		log.Fatal(err)
	}
	_, err = q.ApplyBatch([]deadlinequeue.Change{{Kind: deadlinequeue.Add, Task: deadlinequeue.Task{ID: "email-1", Queue: "notifications", Due: 10, Priority: 2, Payload: []byte("send")}}, {Kind: deadlinequeue.Add, Task: deadlinequeue.Task{ID: "email-2", Queue: "notifications", Due: 10, Priority: 9, Payload: []byte("urgent")}}})
	if err != nil {
		log.Fatal(err)
	}
	due, err := q.PopDue("notifications", 10, 1)
	if err != nil {
		log.Fatal(err)
	}
	s := q.Snapshot()
	fmt.Printf("popped=%s remaining=%d generation=%d payload=%d\n", due[0].ID, s.Tasks, s.Generation, s.PayloadBytes)
}
