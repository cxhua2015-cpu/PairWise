package main

import (
	"example.com/pairwise/resourceledger162/resourceledger162"
	"fmt"
)

func main() {
	s, _ := resourceledger162.New(resourceledger162.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(resourceledger162.Batch{Ops: []resourceledger162.Op{{Kind: resourceledger162.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
