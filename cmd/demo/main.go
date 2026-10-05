package main

import (
	"example.com/pairwise/expirytable489/expirytable489"
	"fmt"
)

func main() {
	t, _ := expirytable489.New(expirytable489.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable489.Batch{Now: 1, Ops: []expirytable489.Op{{Kind: expirytable489.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
