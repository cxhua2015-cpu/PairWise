package main

import (
	"example.com/pairwise/balanceledger472/balanceledger472"
	"fmt"
)

func main() {
	s, _ := balanceledger472.New(balanceledger472.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger472.Batch{Ops: []balanceledger472.Op{{Kind: balanceledger472.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
