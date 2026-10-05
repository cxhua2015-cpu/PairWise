package main

import (
	"example.com/pairwise/expirytable254/expirytable254"
	"fmt"
)

func main() {
	t, _ := expirytable254.New(expirytable254.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable254.Batch{Now: 1, Ops: []expirytable254.Op{{Kind: expirytable254.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
