package main

import (
	"example.com/pairwise/balanceledger257/balanceledger257"
	"fmt"
)

func main() {
	s, _ := balanceledger257.New(balanceledger257.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger257.Batch{Ops: []balanceledger257.Op{{Kind: balanceledger257.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
