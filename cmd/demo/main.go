package main

import (
	"example.com/pairwise/resourceledger197/resourceledger197"
	"fmt"
)

func main() {
	s, _ := resourceledger197.New(resourceledger197.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(resourceledger197.Batch{Ops: []resourceledger197.Op{{Kind: resourceledger197.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
