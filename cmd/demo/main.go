package main

import (
	"example.com/pairwise/expirytable419/expirytable419"
	"fmt"
)

func main() {
	t, _ := expirytable419.New(expirytable419.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable419.Batch{Now: 1, Ops: []expirytable419.Op{{Kind: expirytable419.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
