package main

import (
	"example.com/pairwise/balanceledger222/balanceledger222"
	"fmt"
)

func main() {
	s, _ := balanceledger222.New(balanceledger222.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger222.Batch{Ops: []balanceledger222.Op{{Kind: balanceledger222.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
