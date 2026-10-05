package main

import (
	"example.com/pairwise/resourceledger107/resourceledger107"
	"fmt"
)

func main() {
	s, _ := resourceledger107.New(resourceledger107.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(resourceledger107.Batch{Ops: []resourceledger107.Op{{Kind: resourceledger107.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
