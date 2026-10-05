package main

import (
	"example.com/pairwise/balanceledger372/balanceledger372"
	"fmt"
)

func main() {
	s, _ := balanceledger372.New(balanceledger372.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger372.Batch{Ops: []balanceledger372.Op{{Kind: balanceledger372.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
