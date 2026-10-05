package main

import (
	"example.com/pairwise/expirytable334/expirytable334"
	"fmt"
)

func main() {
	t, _ := expirytable334.New(expirytable334.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable334.Batch{Now: 1, Ops: []expirytable334.Op{{Kind: expirytable334.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
