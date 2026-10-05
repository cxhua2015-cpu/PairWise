package main

import (
	"example.com/pairwise/featureflags/featureflags"
	"fmt"
)

func main() {
	s, _ := featureflags.New(featureflags.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(featureflags.Batch{Ops: []featureflags.Op{{Kind: featureflags.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
