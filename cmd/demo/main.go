package main

import (
	"example.com/pairwise/balanceledger287/balanceledger287"
	"fmt"
)

func main() {
	s, _ := balanceledger287.New(balanceledger287.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger287.Batch{Ops: []balanceledger287.Op{{Kind: balanceledger287.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
