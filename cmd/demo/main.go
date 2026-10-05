package main

import (
	"example.com/pairwise/expirytable269/expirytable269"
	"fmt"
)

func main() {
	t, _ := expirytable269.New(expirytable269.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable269.Batch{Now: 1, Ops: []expirytable269.Op{{Kind: expirytable269.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
