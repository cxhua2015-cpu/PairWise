package main

import (
	"example.com/pairwise/expirytable409/expirytable409"
	"fmt"
)

func main() {
	t, _ := expirytable409.New(expirytable409.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable409.Batch{Now: 1, Ops: []expirytable409.Op{{Kind: expirytable409.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
