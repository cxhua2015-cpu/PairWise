package main

import (
	"example.com/pairwise/expirytable494/expirytable494"
	"fmt"
)

func main() {
	t, _ := expirytable494.New(expirytable494.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable494.Batch{Now: 1, Ops: []expirytable494.Op{{Kind: expirytable494.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
