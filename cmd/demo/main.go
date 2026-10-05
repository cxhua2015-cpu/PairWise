package main

import (
	"example.com/pairwise/balanceledger412/balanceledger412"
	"fmt"
)

func main() {
	s, _ := balanceledger412.New(balanceledger412.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger412.Batch{Ops: []balanceledger412.Op{{Kind: balanceledger412.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
