package main

import (
	"example.com/pairwise/balanceledger432/balanceledger432"
	"fmt"
)

func main() {
	s, _ := balanceledger432.New(balanceledger432.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(balanceledger432.Batch{Ops: []balanceledger432.Op{{Kind: balanceledger432.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
