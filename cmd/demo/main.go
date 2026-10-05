package main

import (
	"example.com/pairwise/resourceledger182/resourceledger182"
	"fmt"
)

func main() {
	s, _ := resourceledger182.New(resourceledger182.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(resourceledger182.Batch{Ops: []resourceledger182.Op{{Kind: resourceledger182.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
