package main

import (
	"example.com/pairwise/resourceledger087/resourceledger087"
	"fmt"
)

func main() {
	s, _ := resourceledger087.New(resourceledger087.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(resourceledger087.Batch{Ops: []resourceledger087.Op{{Kind: resourceledger087.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
