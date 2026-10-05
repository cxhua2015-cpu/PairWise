package main

import (
	"example.com/pairwise/resourceledger167/resourceledger167"
	"fmt"
)

func main() {
	s, _ := resourceledger167.New(resourceledger167.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(resourceledger167.Batch{Ops: []resourceledger167.Op{{Kind: resourceledger167.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
