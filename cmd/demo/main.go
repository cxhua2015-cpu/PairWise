package main

import (
	"example.com/pairwise/balanceledger227/balanceledger227"
	"fmt"
)

func main() {
	s, _ := balanceledger227.New(balanceledger227.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger227.Batch{Ops: []balanceledger227.Op{{Kind: balanceledger227.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
