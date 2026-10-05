package main

import (
	"example.com/pairwise/balanceledger202/balanceledger202"
	"fmt"
)

func main() {
	s, _ := balanceledger202.New(balanceledger202.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger202.Batch{Ops: []balanceledger202.Op{{Kind: balanceledger202.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
