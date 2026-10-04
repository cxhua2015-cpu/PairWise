package main

import (
	"example.com/pairwise/tokenbucket/tokenbucket"
	"fmt"
	"log"
)

func main() {
	r, err := tokenbucket.New(tokenbucket.Options{MaxBuckets: 4, MaxNameBytes: 16}, []tokenbucket.BucketSpec{{Name: "api", Capacity: 10, RefillTokens: 3, RefillEvery: 5}})
	if err != nil {
		log.Fatal(err)
	}
	_, err = r.ApplyBatch([]tokenbucket.Change{{Kind: tokenbucket.Acquire, Bucket: "api", Tokens: 8, At: 0}, {Kind: tokenbucket.Acquire, Bucket: "api", Tokens: 3, At: 5}})
	if err != nil {
		log.Fatal(err)
	}
	s, err := r.Inspect("api", 10)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("tokens=%d lastRefill=%d nextRefill=%d generation=%d\n", s.Tokens, s.LastRefill, s.NextRefill, r.Snapshot().Generation)
}
