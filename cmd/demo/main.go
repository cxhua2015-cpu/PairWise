package main

import (
	"example.com/pairwise/usageledger/usageledger"
	"fmt"
)

func main() {
	s, _ := usageledger.New(usageledger.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(usageledger.Batch{Ops: []usageledger.Op{{Kind: usageledger.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
