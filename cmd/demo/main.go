package main

import (
	"example.com/pairwise/expirytable214/expirytable214"
	"fmt"
)

func main() {
	t, _ := expirytable214.New(expirytable214.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable214.Batch{Now: 1, Ops: []expirytable214.Op{{Kind: expirytable214.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
