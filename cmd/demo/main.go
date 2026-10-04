package main

import (
	"fmt"

	"example.com/pairwise/expiringstore/expiringstore"
)

func main() {
	s, _ := expiringstore.New(expiringstore.Options{MaxEntries: 8, MaxTotalBytes: 64, MaxValueBytes: 16, MaxKeyBytes: 16})
	r, _ := s.Apply(expiringstore.Batch{Now: 2, Ops: []expiringstore.Op{
		{Kind: expiringstore.Put, Key: "cfg/a", Value: []byte("one"), ExpiresAt: 5},
		{Kind: expiringstore.Put, Key: "cfg/b", Value: []byte("two"), ExpiresAt: 8},
	}})
	expired, _ := s.Sweep(5)
	x := s.Snapshot()
	fmt.Printf("generation=%d revision=%d expired=%d remaining=%d now=%d\n", x.Generation, r.Revision, len(expired), len(x.Entries), x.Now)
}
