package main

import (
	"example.com/pairwise/balanceledger387/balanceledger387"
	"fmt"
)

func main() {
	s, _ := balanceledger387.New(balanceledger387.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger387.Batch{Ops: []balanceledger387.Op{{Kind: balanceledger387.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
