package main

import (
	"example.com/pairwise/resourceledger122/resourceledger122"
	"fmt"
)

func main() {
	s, _ := resourceledger122.New(resourceledger122.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(resourceledger122.Batch{Ops: []resourceledger122.Op{{Kind: resourceledger122.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
