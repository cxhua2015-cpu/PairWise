package main

import (
	"example.com/pairwise/balanceledger242/balanceledger242"
	"fmt"
)

func main() {
	s, _ := balanceledger242.New(balanceledger242.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger242.Batch{Ops: []balanceledger242.Op{{Kind: balanceledger242.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
