package main

import (
	"example.com/pairwise/resourceledger172/resourceledger172"
	"fmt"
)

func main() {
	s, _ := resourceledger172.New(resourceledger172.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(resourceledger172.Batch{Ops: []resourceledger172.Op{{Kind: resourceledger172.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
