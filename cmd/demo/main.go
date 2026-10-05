package main

import (
	"example.com/pairwise/expirytable234/expirytable234"
	"fmt"
)

func main() {
	t, _ := expirytable234.New(expirytable234.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable234.Batch{Now: 1, Ops: []expirytable234.Op{{Kind: expirytable234.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
