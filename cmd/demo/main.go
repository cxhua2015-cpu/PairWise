package main

import (
	"example.com/pairwise/metacatalog291/metacatalog291"
	"fmt"
)

func main() {
	s, _ := metacatalog291.New(metacatalog291.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog291.Batch{Ops: []metacatalog291.Op{{Kind: metacatalog291.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
