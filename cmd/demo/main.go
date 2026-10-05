package main

import (
	"example.com/pairwise/expirytable274/expirytable274"
	"fmt"
)

func main() {
	t, _ := expirytable274.New(expirytable274.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable274.Batch{Now: 1, Ops: []expirytable274.Op{{Kind: expirytable274.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
