package main

import (
	"example.com/pairwise/balanceledger282/balanceledger282"
	"fmt"
)

func main() {
	s, _ := balanceledger282.New(balanceledger282.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger282.Batch{Ops: []balanceledger282.Op{{Kind: balanceledger282.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
