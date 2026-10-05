package main

import (
	"example.com/pairwise/balanceledger207/balanceledger207"
	"fmt"
)

func main() {
	s, _ := balanceledger207.New(balanceledger207.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger207.Batch{Ops: []balanceledger207.Op{{Kind: balanceledger207.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
