package main

import (
	"example.com/pairwise/balanceledger337/balanceledger337"
	"fmt"
)

func main() {
	s, _ := balanceledger337.New(balanceledger337.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger337.Batch{Ops: []balanceledger337.Op{{Kind: balanceledger337.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
