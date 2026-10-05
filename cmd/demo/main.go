package main

import (
	"example.com/pairwise/expirytable369/expirytable369"
	"fmt"
)

func main() {
	t, _ := expirytable369.New(expirytable369.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable369.Batch{Now: 1, Ops: []expirytable369.Op{{Kind: expirytable369.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
