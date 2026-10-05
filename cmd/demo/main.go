package main

import (
	"example.com/pairwise/quotaaccount/quotaaccount"
	"fmt"
)

func main() {
	s, _ := quotaaccount.New(quotaaccount.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(quotaaccount.Batch{Ops: []quotaaccount.Op{{Kind: quotaaccount.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
