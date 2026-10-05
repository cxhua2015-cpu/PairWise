package main

import (
	"example.com/pairwise/resourceledger177/resourceledger177"
	"fmt"
)

func main() {
	s, _ := resourceledger177.New(resourceledger177.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(resourceledger177.Batch{Ops: []resourceledger177.Op{{Kind: resourceledger177.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
