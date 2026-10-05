package main

import (
	"example.com/pairwise/balanceledger327/balanceledger327"
	"fmt"
)

func main() {
	s, _ := balanceledger327.New(balanceledger327.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger327.Batch{Ops: []balanceledger327.Op{{Kind: balanceledger327.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
