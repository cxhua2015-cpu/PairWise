package main

import (
	"example.com/pairwise/resourceledger192/resourceledger192"
	"fmt"
)

func main() {
	s, _ := resourceledger192.New(resourceledger192.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(resourceledger192.Batch{Ops: []resourceledger192.Op{{Kind: resourceledger192.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
