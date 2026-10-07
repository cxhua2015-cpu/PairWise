package main

import (
	"example.com/pairwise/expirytable434/expirytable434"
	"fmt"
)

func main() {
	t, _ := expirytable434.New(expirytable434.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable434.Batch{Now: 1, Ops: []expirytable434.Op{{Kind: expirytable434.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
