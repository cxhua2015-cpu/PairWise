package main

import (
	"example.com/pairwise/balanceledger392/balanceledger392"
	"fmt"
)

func main() {
	s, _ := balanceledger392.New(balanceledger392.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger392.Batch{Ops: []balanceledger392.Op{{Kind: balanceledger392.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
