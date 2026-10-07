package main

import (
	"example.com/pairwise/balanceledger407/balanceledger407"
	"fmt"
)

func main() {
	s, _ := balanceledger407.New(balanceledger407.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger407.Batch{Ops: []balanceledger407.Op{{Kind: balanceledger407.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
