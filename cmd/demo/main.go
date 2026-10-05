package main

import (
	"example.com/pairwise/expirytable209/expirytable209"
	"fmt"
)

func main() {
	t, _ := expirytable209.New(expirytable209.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable209.Batch{Now: 1, Ops: []expirytable209.Op{{Kind: expirytable209.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
