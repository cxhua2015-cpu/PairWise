package main

import (
	"example.com/pairwise/balanceledger342/balanceledger342"
	"fmt"
)

func main() {
	s, _ := balanceledger342.New(balanceledger342.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger342.Batch{Ops: []balanceledger342.Op{{Kind: balanceledger342.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
