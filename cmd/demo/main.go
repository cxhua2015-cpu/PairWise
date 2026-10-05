package main

import (
	"example.com/pairwise/balanceledger497/balanceledger497"
	"fmt"
)

func main() {
	s, _ := balanceledger497.New(balanceledger497.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger497.Batch{Ops: []balanceledger497.Op{{Kind: balanceledger497.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
