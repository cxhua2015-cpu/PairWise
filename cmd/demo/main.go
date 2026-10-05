package main

import (
	"example.com/pairwise/balanceledger217/balanceledger217"
	"fmt"
)

func main() {
	s, _ := balanceledger217.New(balanceledger217.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger217.Batch{Ops: []balanceledger217.Op{{Kind: balanceledger217.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
