package main

import (
	"example.com/pairwise/balanceledger457/balanceledger457"
	"fmt"
)

func main() {
	s, _ := balanceledger457.New(balanceledger457.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger457.Batch{Ops: []balanceledger457.Op{{Kind: balanceledger457.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
