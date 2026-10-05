package main

import (
	"example.com/pairwise/balanceledger397/balanceledger397"
	"fmt"
)

func main() {
	s, _ := balanceledger397.New(balanceledger397.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger397.Batch{Ops: []balanceledger397.Op{{Kind: balanceledger397.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
