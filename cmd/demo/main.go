package main

import (
	"example.com/pairwise/balanceledger462/balanceledger462"
	"fmt"
)

func main() {
	s, _ := balanceledger462.New(balanceledger462.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger462.Batch{Ops: []balanceledger462.Op{{Kind: balanceledger462.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
