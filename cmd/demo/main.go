package main

import (
	"example.com/pairwise/inventory/inventory"
	"fmt"
)

func main() {
	s, _ := inventory.New(inventory.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(inventory.Batch{Ops: []inventory.Op{{Kind: inventory.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
