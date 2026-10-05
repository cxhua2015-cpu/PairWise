package main

import (
	"example.com/pairwise/expirytable299/expirytable299"
	"fmt"
)

func main() {
	t, _ := expirytable299.New(expirytable299.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable299.Batch{Now: 1, Ops: []expirytable299.Op{{Kind: expirytable299.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
