package main

import (
	"example.com/pairwise/resourceledger152/resourceledger152"
	"fmt"
)

func main() {
	s, _ := resourceledger152.New(resourceledger152.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(resourceledger152.Batch{Ops: []resourceledger152.Op{{Kind: resourceledger152.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
