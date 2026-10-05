package main

import (
	"example.com/pairwise/metacatalog336/metacatalog336"
	"fmt"
)

func main() {
	s, _ := metacatalog336.New(metacatalog336.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog336.Batch{Ops: []metacatalog336.Op{{Kind: metacatalog336.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
