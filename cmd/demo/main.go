package main

import (
	"example.com/pairwise/reservationlease/reservationlease"
	"fmt"
)

func main() {
	t, _ := reservationlease.New(reservationlease.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(reservationlease.Batch{Now: 1, Ops: []reservationlease.Op{{Kind: reservationlease.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
