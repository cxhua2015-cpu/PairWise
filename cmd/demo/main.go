package main

import (
	"example.com/pairwise/expirytable304/expirytable304"
	"fmt"
)

func main() {
	t, _ := expirytable304.New(expirytable304.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable304.Batch{Now: 1, Ops: []expirytable304.Op{{Kind: expirytable304.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
