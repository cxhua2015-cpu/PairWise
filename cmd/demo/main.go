package main

import (
	"example.com/pairwise/expirytable319/expirytable319"
	"fmt"
)

func main() {
	t, _ := expirytable319.New(expirytable319.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable319.Batch{Now: 1, Ops: []expirytable319.Op{{Kind: expirytable319.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
