package main

import (
	"example.com/pairwise/resourceledger187/resourceledger187"
	"fmt"
)

func main() {
	s, _ := resourceledger187.New(resourceledger187.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(resourceledger187.Batch{Ops: []resourceledger187.Op{{Kind: resourceledger187.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
