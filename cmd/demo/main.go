package main

import (
	"example.com/pairwise/balanceledger442/balanceledger442"
	"fmt"
)

func main() {
	s, _ := balanceledger442.New(balanceledger442.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger442.Batch{Ops: []balanceledger442.Op{{Kind: balanceledger442.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
