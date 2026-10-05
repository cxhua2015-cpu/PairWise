package main

import (
	"example.com/pairwise/balanceledger467/balanceledger467"
	"fmt"
)

func main() {
	s, _ := balanceledger467.New(balanceledger467.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger467.Batch{Ops: []balanceledger467.Op{{Kind: balanceledger467.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
