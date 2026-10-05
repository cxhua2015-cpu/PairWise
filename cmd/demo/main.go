package main

import (
	"example.com/pairwise/balanceledger452/balanceledger452"
	"fmt"
)

func main() {
	s, _ := balanceledger452.New(balanceledger452.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger452.Batch{Ops: []balanceledger452.Op{{Kind: balanceledger452.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
