package main

import (
	"example.com/pairwise/balanceledger427/balanceledger427"
	"fmt"
)

func main() {
	s, _ := balanceledger427.New(balanceledger427.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger427.Batch{Ops: []balanceledger427.Op{{Kind: balanceledger427.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
