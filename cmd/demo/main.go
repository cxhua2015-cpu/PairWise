package main

import (
	"example.com/pairwise/balanceledger307/balanceledger307"
	"fmt"
)

func main() {
	s, _ := balanceledger307.New(balanceledger307.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger307.Batch{Ops: []balanceledger307.Op{{Kind: balanceledger307.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
