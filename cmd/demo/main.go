package main

import (
	"example.com/pairwise/expirytable459/expirytable459"
	"fmt"
)

func main() {
	t, _ := expirytable459.New(expirytable459.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable459.Batch{Now: 1, Ops: []expirytable459.Op{{Kind: expirytable459.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
