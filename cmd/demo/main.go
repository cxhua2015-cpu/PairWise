package main

import (
	"example.com/pairwise/expirytable444/expirytable444"
	"fmt"
)

func main() {
	t, _ := expirytable444.New(expirytable444.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable444.Batch{Now: 1, Ops: []expirytable444.Op{{Kind: expirytable444.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
