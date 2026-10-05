package main

import (
	"example.com/pairwise/resourceledger142/resourceledger142"
	"fmt"
)

func main() {
	s, _ := resourceledger142.New(resourceledger142.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(resourceledger142.Batch{Ops: []resourceledger142.Op{{Kind: resourceledger142.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
