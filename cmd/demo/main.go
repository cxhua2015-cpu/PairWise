package main

import (
	"example.com/pairwise/expirytable474/expirytable474"
	"fmt"
)

func main() {
	t, _ := expirytable474.New(expirytable474.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable474.Batch{Now: 1, Ops: []expirytable474.Op{{Kind: expirytable474.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
