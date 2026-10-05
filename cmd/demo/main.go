package main

import (
	"example.com/pairwise/balanceledger482/balanceledger482"
	"fmt"
)

func main() {
	s, _ := balanceledger482.New(balanceledger482.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger482.Batch{Ops: []balanceledger482.Op{{Kind: balanceledger482.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
