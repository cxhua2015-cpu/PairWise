package main

import (
	"example.com/pairwise/balanceledger322/balanceledger322"
	"fmt"
)

func main() {
	s, _ := balanceledger322.New(balanceledger322.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger322.Batch{Ops: []balanceledger322.Op{{Kind: balanceledger322.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
