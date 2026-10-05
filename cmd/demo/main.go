package main

import (
	"example.com/pairwise/shardbalance/shardbalance"
	"fmt"
)

func main() {
	s, _ := shardbalance.New(shardbalance.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(shardbalance.Batch{Ops: []shardbalance.Op{{Kind: shardbalance.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
