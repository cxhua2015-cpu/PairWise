package main

import (
	"example.com/pairwise/balanceledger352/balanceledger352"
	"fmt"
)

func main() {
	s, _ := balanceledger352.New(balanceledger352.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger352.Batch{Ops: []balanceledger352.Op{{Kind: balanceledger352.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
