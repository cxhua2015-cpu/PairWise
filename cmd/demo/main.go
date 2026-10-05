package main

import (
	"example.com/pairwise/rewardledger/rewardledger"
	"fmt"
)

func main() {
	s, _ := rewardledger.New(rewardledger.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(rewardledger.Batch{Ops: []rewardledger.Op{{Kind: rewardledger.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
