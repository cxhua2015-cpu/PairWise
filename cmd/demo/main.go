package main

import (
	"example.com/pairwise/balanceledger277/balanceledger277"
	"fmt"
)

func main() {
	s, _ := balanceledger277.New(balanceledger277.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger277.Batch{Ops: []balanceledger277.Op{{Kind: balanceledger277.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
