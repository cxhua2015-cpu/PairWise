package main

import (
	"example.com/pairwise/resourceledger092/resourceledger092"
	"fmt"
)

func main() {
	s, _ := resourceledger092.New(resourceledger092.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(resourceledger092.Batch{Ops: []resourceledger092.Op{{Kind: resourceledger092.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
