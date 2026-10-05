package main

import (
	"example.com/pairwise/balanceledger252/balanceledger252"
	"fmt"
)

func main() {
	s, _ := balanceledger252.New(balanceledger252.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger252.Batch{Ops: []balanceledger252.Op{{Kind: balanceledger252.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
