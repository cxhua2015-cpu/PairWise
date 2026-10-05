package main

import (
	"example.com/pairwise/resourceledger117/resourceledger117"
	"fmt"
)

func main() {
	s, _ := resourceledger117.New(resourceledger117.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(resourceledger117.Batch{Ops: []resourceledger117.Op{{Kind: resourceledger117.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
