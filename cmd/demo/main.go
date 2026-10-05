package main

import (
	"example.com/pairwise/resourceledger112/resourceledger112"
	"fmt"
)

func main() {
	s, _ := resourceledger112.New(resourceledger112.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(resourceledger112.Batch{Ops: []resourceledger112.Op{{Kind: resourceledger112.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
