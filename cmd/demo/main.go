package main

import (
	"example.com/pairwise/balanceledger437/balanceledger437"
	"fmt"
)

func main() {
	s, _ := balanceledger437.New(balanceledger437.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger437.Batch{Ops: []balanceledger437.Op{{Kind: balanceledger437.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
