package main

import (
	"example.com/pairwise/resourceledger082/resourceledger082"
	"fmt"
)

func main() {
	s, _ := resourceledger082.New(resourceledger082.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(resourceledger082.Batch{Ops: []resourceledger082.Op{{Kind: resourceledger082.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
