package main

import (
	"example.com/pairwise/balanceledger477/balanceledger477"
	"fmt"
)

func main() {
	s, _ := balanceledger477.New(balanceledger477.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger477.Batch{Ops: []balanceledger477.Op{{Kind: balanceledger477.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
