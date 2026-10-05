package main

import (
	"example.com/pairwise/balanceledger377/balanceledger377"
	"fmt"
)

func main() {
	s, _ := balanceledger377.New(balanceledger377.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger377.Batch{Ops: []balanceledger377.Op{{Kind: balanceledger377.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
