package main

import (
	"example.com/pairwise/balanceledger492/balanceledger492"
	"fmt"
)

func main() {
	s, _ := balanceledger492.New(balanceledger492.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger492.Batch{Ops: []balanceledger492.Op{{Kind: balanceledger492.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
