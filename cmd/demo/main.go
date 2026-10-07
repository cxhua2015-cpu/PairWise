package main

import (
	"example.com/pairwise/balanceledger402/balanceledger402"
	"fmt"
)

func main() {
	s, _ := balanceledger402.New(balanceledger402.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger402.Batch{Ops: []balanceledger402.Op{{Kind: balanceledger402.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
