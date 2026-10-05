package main

import (
	"example.com/pairwise/balanceledger312/balanceledger312"
	"fmt"
)

func main() {
	s, _ := balanceledger312.New(balanceledger312.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger312.Batch{Ops: []balanceledger312.Op{{Kind: balanceledger312.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
