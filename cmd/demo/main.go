package main

import (
	"example.com/pairwise/reorder/reorder"
	"fmt"
	"log"
)

func main() {
	b, err := reorder.New(reorder.Options{MaxStreams: 4, MaxBuffered: 16, MaxPayloadBytes: 128, MaxStreamBytes: 32})
	if err != nil {
		log.Fatal(err)
	}
	if _, err = b.PushBatch([]reorder.Event{{Stream: "orders", Sequence: 2, Payload: []byte("second")}}); err != nil {
		log.Fatal(err)
	}
	ready, err := b.PushBatch([]reorder.Event{{Stream: "orders", Sequence: 1, Payload: []byte("first")}})
	if err != nil {
		log.Fatal(err)
	}
	s := b.Snapshot()
	fmt.Printf("ready=%d first=%s last=%s next=%d buffered=%d generation=%d\n", len(ready), ready[0].Payload, ready[1].Payload, s.State[0].Next, s.Buffered, s.Generation)
}
