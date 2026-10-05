package main

import (
	"example.com/pairwise/capacityledger/capacityledger"
	"fmt"
)

func main() {
	s, _ := capacityledger.New(capacityledger.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(capacityledger.Batch{Ops: []capacityledger.Op{{Kind: capacityledger.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
