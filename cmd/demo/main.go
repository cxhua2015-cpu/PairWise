package main

import (
	"example.com/pairwise/balanceledger447/balanceledger447"
	"fmt"
)

func main() {
	s, _ := balanceledger447.New(balanceledger447.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger447.Batch{Ops: []balanceledger447.Op{{Kind: balanceledger447.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
