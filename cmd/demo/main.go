package main

import (
	"example.com/pairwise/resourceledger157/resourceledger157"
	"fmt"
)

func main() {
	s, _ := resourceledger157.New(resourceledger157.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(resourceledger157.Batch{Ops: []resourceledger157.Op{{Kind: resourceledger157.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
