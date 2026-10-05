package main

import (
	"example.com/pairwise/balanceledger237/balanceledger237"
	"fmt"
)

func main() {
	s, _ := balanceledger237.New(balanceledger237.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger237.Batch{Ops: []balanceledger237.Op{{Kind: balanceledger237.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
