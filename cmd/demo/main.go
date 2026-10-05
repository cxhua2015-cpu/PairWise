package main

import (
	"example.com/pairwise/creditpool/creditpool"
	"fmt"
)

func main() {
	s, _ := creditpool.New(creditpool.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(creditpool.Batch{Ops: []creditpool.Op{{Kind: creditpool.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
