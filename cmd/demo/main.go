package main

import (
	"example.com/pairwise/costledger/costledger"
	"fmt"
)

func main() {
	s, _ := costledger.New(costledger.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(costledger.Batch{Ops: []costledger.Op{{Kind: costledger.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
