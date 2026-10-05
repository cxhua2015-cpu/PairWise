package main

import (
	"example.com/pairwise/balanceledger297/balanceledger297"
	"fmt"
)

func main() {
	s, _ := balanceledger297.New(balanceledger297.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger297.Batch{Ops: []balanceledger297.Op{{Kind: balanceledger297.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
