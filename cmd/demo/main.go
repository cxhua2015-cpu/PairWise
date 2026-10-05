package main

import (
	"example.com/pairwise/budgetledger/budgetledger"
	"fmt"
)

func main() {
	s, _ := budgetledger.New(budgetledger.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(budgetledger.Batch{Ops: []budgetledger.Op{{Kind: budgetledger.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
