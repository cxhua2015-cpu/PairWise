package main

import (
	"example.com/pairwise/balanceledger292/balanceledger292"
	"fmt"
)

func main() {
	s, _ := balanceledger292.New(balanceledger292.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger292.Batch{Ops: []balanceledger292.Op{{Kind: balanceledger292.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
