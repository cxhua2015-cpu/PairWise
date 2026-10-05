package main

import (
	"example.com/pairwise/balanceledger247/balanceledger247"
	"fmt"
)

func main() {
	s, _ := balanceledger247.New(balanceledger247.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger247.Batch{Ops: []balanceledger247.Op{{Kind: balanceledger247.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
