package main

import (
	"example.com/pairwise/resourceledger127/resourceledger127"
	"fmt"
)

func main() {
	s, _ := resourceledger127.New(resourceledger127.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(resourceledger127.Batch{Ops: []resourceledger127.Op{{Kind: resourceledger127.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
