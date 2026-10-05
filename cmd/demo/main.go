package main

import (
	"example.com/pairwise/balanceledger357/balanceledger357"
	"fmt"
)

func main() {
	s, _ := balanceledger357.New(balanceledger357.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger357.Batch{Ops: []balanceledger357.Op{{Kind: balanceledger357.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
