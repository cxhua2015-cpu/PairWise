package main

import (
	"example.com/pairwise/secretcatalog/secretcatalog"
	"fmt"
)

func main() {
	s, _ := secretcatalog.New(secretcatalog.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(secretcatalog.Batch{Ops: []secretcatalog.Op{{Kind: secretcatalog.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
