package main

import (
	"example.com/pairwise/balanceledger487/balanceledger487"
	"fmt"
)

func main() {
	s, _ := balanceledger487.New(balanceledger487.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger487.Batch{Ops: []balanceledger487.Op{{Kind: balanceledger487.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
