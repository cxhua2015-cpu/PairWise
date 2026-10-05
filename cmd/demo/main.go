package main

import (
	"example.com/pairwise/resourceledger137/resourceledger137"
	"fmt"
)

func main() {
	s, _ := resourceledger137.New(resourceledger137.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(resourceledger137.Batch{Ops: []resourceledger137.Op{{Kind: resourceledger137.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
