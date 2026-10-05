package main

import (
	"example.com/pairwise/balanceledger212/balanceledger212"
	"fmt"
)

func main() {
	s, _ := balanceledger212.New(balanceledger212.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger212.Batch{Ops: []balanceledger212.Op{{Kind: balanceledger212.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
