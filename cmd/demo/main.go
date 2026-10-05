package main

import (
	"example.com/pairwise/tokenledger/tokenledger"
	"fmt"
)

func main() {
	s, _ := tokenledger.New(tokenledger.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(tokenledger.Batch{Ops: []tokenledger.Op{{Kind: tokenledger.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
