package main

import (
	"example.com/pairwise/resourceledger097/resourceledger097"
	"fmt"
)

func main() {
	s, _ := resourceledger097.New(resourceledger097.Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 100})
	x, _ := s.Apply(resourceledger097.Batch{Ops: []resourceledger097.Op{{Kind: resourceledger097.Add, Name: "alpha", Delta: 7}}})
	fmt.Printf("generation=%d revision=%d accounts=%d\n", x.Generation, x.Revision, len(s.Snapshot().Accounts))
}
