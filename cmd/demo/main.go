package main

import (
	"example.com/pairwise/balanceledger382/balanceledger382"
	"fmt"
)

func main() {
	s, _ := balanceledger382.New(balanceledger382.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger382.Batch{Ops: []balanceledger382.Op{{Kind: balanceledger382.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
