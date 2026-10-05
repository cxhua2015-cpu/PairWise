package main

import (
	"example.com/pairwise/expirytable264/expirytable264"
	"fmt"
)

func main() {
	t, _ := expirytable264.New(expirytable264.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable264.Batch{Now: 1, Ops: []expirytable264.Op{{Kind: expirytable264.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
