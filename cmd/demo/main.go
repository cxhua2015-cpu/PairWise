package main

import (
	"example.com/pairwise/balanceledger347/balanceledger347"
	"fmt"
)

func main() {
	s, _ := balanceledger347.New(balanceledger347.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger347.Batch{Ops: []balanceledger347.Op{{Kind: balanceledger347.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
