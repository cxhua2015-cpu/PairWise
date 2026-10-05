package main

import (
	"example.com/pairwise/balanceledger262/balanceledger262"
	"fmt"
)

func main() {
	s, _ := balanceledger262.New(balanceledger262.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger262.Batch{Ops: []balanceledger262.Op{{Kind: balanceledger262.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
