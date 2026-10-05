package main

import (
	"example.com/pairwise/expirytable389/expirytable389"
	"fmt"
)

func main() {
	t, _ := expirytable389.New(expirytable389.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable389.Batch{Now: 1, Ops: []expirytable389.Op{{Kind: expirytable389.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
