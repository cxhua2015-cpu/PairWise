package main

import (
	"example.com/pairwise/expirytable339/expirytable339"
	"fmt"
)

func main() {
	t, _ := expirytable339.New(expirytable339.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable339.Batch{Now: 1, Ops: []expirytable339.Op{{Kind: expirytable339.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
