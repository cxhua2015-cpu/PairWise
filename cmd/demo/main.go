package main

import (
	"example.com/pairwise/balanceledger332/balanceledger332"
	"fmt"
)

func main() {
	s, _ := balanceledger332.New(balanceledger332.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger332.Batch{Ops: []balanceledger332.Op{{Kind: balanceledger332.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
