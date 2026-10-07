package main

import (
	"example.com/pairwise/balanceledger422/balanceledger422"
	"fmt"
)

func main() {
	s, _ := balanceledger422.New(balanceledger422.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger422.Batch{Ops: []balanceledger422.Op{{Kind: balanceledger422.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
