package main

import (
	"example.com/pairwise/expirytable384/expirytable384"
	"fmt"
)

func main() {
	t, _ := expirytable384.New(expirytable384.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable384.Batch{Now: 1, Ops: []expirytable384.Op{{Kind: expirytable384.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
