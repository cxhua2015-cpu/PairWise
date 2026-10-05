package main

import (
	"example.com/pairwise/expirytable354/expirytable354"
	"fmt"
)

func main() {
	t, _ := expirytable354.New(expirytable354.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable354.Batch{Now: 1, Ops: []expirytable354.Op{{Kind: expirytable354.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
