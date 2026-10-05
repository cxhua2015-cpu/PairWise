package main

import (
	"example.com/pairwise/balanceledger302/balanceledger302"
	"fmt"
)

func main() {
	s, _ := balanceledger302.New(balanceledger302.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger302.Batch{Ops: []balanceledger302.Op{{Kind: balanceledger302.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
