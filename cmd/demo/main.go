package main

import (
	"example.com/pairwise/balanceledger362/balanceledger362"
	"fmt"
)

func main() {
	s, _ := balanceledger362.New(balanceledger362.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger362.Batch{Ops: []balanceledger362.Op{{Kind: balanceledger362.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
