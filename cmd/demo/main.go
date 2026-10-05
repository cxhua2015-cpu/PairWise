package main

import (
	"example.com/pairwise/expirytable499/expirytable499"
	"fmt"
)

func main() {
	t, _ := expirytable499.New(expirytable499.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable499.Batch{Now: 1, Ops: []expirytable499.Op{{Kind: expirytable499.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
