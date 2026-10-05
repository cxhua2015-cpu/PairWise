package main

import (
	"example.com/pairwise/balanceledger232/balanceledger232"
	"fmt"
)

func main() {
	s, _ := balanceledger232.New(balanceledger232.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger232.Batch{Ops: []balanceledger232.Op{{Kind: balanceledger232.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
