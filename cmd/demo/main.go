package main

import (
	"example.com/pairwise/expirytable294/expirytable294"
	"fmt"
)

func main() {
	t, _ := expirytable294.New(expirytable294.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable294.Batch{Now: 1, Ops: []expirytable294.Op{{Kind: expirytable294.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
