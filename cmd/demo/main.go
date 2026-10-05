package main

import (
	"example.com/pairwise/expirytable379/expirytable379"
	"fmt"
)

func main() {
	t, _ := expirytable379.New(expirytable379.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable379.Batch{Now: 1, Ops: []expirytable379.Op{{Kind: expirytable379.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
