package main

import (
	"example.com/pairwise/balanceledger417/balanceledger417"
	"fmt"
)

func main() {
	s, _ := balanceledger417.New(balanceledger417.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger417.Batch{Ops: []balanceledger417.Op{{Kind: balanceledger417.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
