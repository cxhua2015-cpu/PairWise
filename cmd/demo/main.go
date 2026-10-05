package main

import (
	"example.com/pairwise/expirytable314/expirytable314"
	"fmt"
)

func main() {
	t, _ := expirytable314.New(expirytable314.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable314.Batch{Now: 1, Ops: []expirytable314.Op{{Kind: expirytable314.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
