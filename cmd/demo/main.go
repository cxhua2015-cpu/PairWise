package main

import (
	"fmt"
	"log"

	"example.com/pairwise/idempotency/idempotency"
)

func main() {
	r, err := idempotency.New(idempotency.Options{MaxEntries: 4, MaxResultBytes: 64, MaxKeyBytes: 32})
	if err != nil {
		log.Fatal(err)
	}
	b, err := r.Begin("payment-7", "sha256:abc", 100, 20)
	if err != nil {
		log.Fatal(err)
	}
	if err := r.Complete("payment-7", b.Token, []byte("approved"), 110, 50); err != nil {
		log.Fatal(err)
	}
	replay, err := r.Begin("payment-7", "sha256:abc", 120, 20)
	if err != nil {
		log.Fatal(err)
	}
	s := r.Snapshot()
	fmt.Printf("leader=%t replay=%t result=%s generation=%d entries=%d\n", b.Leader, replay.Replay, replay.Result, s.Generation, s.Entries)
}
