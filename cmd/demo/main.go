package main

import (
	"example.com/pairwise/expirytable279/expirytable279"
	"fmt"
)

func main() {
	t, _ := expirytable279.New(expirytable279.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable279.Batch{Now: 1, Ops: []expirytable279.Op{{Kind: expirytable279.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
