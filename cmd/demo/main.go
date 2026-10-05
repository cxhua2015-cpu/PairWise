package main

import (
	"example.com/pairwise/resourceledger147/resourceledger147"
	"fmt"
)

func main() {
	s, _ := resourceledger147.New(resourceledger147.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(resourceledger147.Batch{Ops: []resourceledger147.Op{{Kind: resourceledger147.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
