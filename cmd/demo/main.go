package main

import (
	"example.com/pairwise/balanceledger267/balanceledger267"
	"fmt"
)

func main() {
	s, _ := balanceledger267.New(balanceledger267.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger267.Batch{Ops: []balanceledger267.Op{{Kind: balanceledger267.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
