package main

import (
	"example.com/pairwise/rangelock/rangelock"
	"fmt"
	"log"
)

func main() {
	m, err := rangelock.New(rangelock.Options{MaxLocks: 8, MaxOwners: 4, MaxMetadataBytes: 64, MaxNameBytes: 32})
	if err != nil {
		log.Fatal(err)
	}
	leases, generation, err := m.AcquireBatch(100, []rangelock.Request{{ID: "reader-1", Owner: "worker-a", Resource: "segment", Start: 0, End: 10, Mode: rangelock.Read, TTL: 20, Metadata: []byte("scan")}})
	if err != nil {
		log.Fatal(err)
	}
	q, err := m.Query("segment", 5, 6, 110)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("generation=%d token=%d matches=%d expires=%d\n", generation, leases[0].Token, len(q), leases[0].ExpiresAt)
}
