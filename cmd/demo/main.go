package main

import (
	"example.com/pairwise/resourceledger132/resourceledger132"
	"fmt"
)

func main() {
	s, _ := resourceledger132.New(resourceledger132.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(resourceledger132.Batch{Ops: []resourceledger132.Op{{Kind: resourceledger132.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
