package main

import (
	"example.com/pairwise/balanceledger272/balanceledger272"
	"fmt"
)

func main() {
	s, _ := balanceledger272.New(balanceledger272.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger272.Batch{Ops: []balanceledger272.Op{{Kind: balanceledger272.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
