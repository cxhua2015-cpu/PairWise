package main

import (
	"example.com/pairwise/balanceledger317/balanceledger317"
	"fmt"
)

func main() {
	s, _ := balanceledger317.New(balanceledger317.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger317.Batch{Ops: []balanceledger317.Op{{Kind: balanceledger317.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
