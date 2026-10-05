package main

import (
	"example.com/pairwise/expirytable429/expirytable429"
	"fmt"
)

func main() {
	t, _ := expirytable429.New(expirytable429.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable429.Batch{Now: 1, Ops: []expirytable429.Op{{Kind: expirytable429.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
