package main

import (
	"example.com/pairwise/resourceledger102/resourceledger102"
	"fmt"
)

func main() {
	s, _ := resourceledger102.New(resourceledger102.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(resourceledger102.Batch{Ops: []resourceledger102.Op{{Kind: resourceledger102.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
