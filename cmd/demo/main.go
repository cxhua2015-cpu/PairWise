package main

import (
	"example.com/pairwise/balanceledger367/balanceledger367"
	"fmt"
)

func main() {
	s, _ := balanceledger367.New(balanceledger367.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger367.Batch{Ops: []balanceledger367.Op{{Kind: balanceledger367.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
