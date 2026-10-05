package main

import (
	"example.com/pairwise/expirytable404/expirytable404"
	"fmt"
)

func main() {
	t, _ := expirytable404.New(expirytable404.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable404.Batch{Now: 1, Ops: []expirytable404.Op{{Kind: expirytable404.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
