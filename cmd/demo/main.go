package main

import (
	"example.com/pairwise/expirytable224/expirytable224"
	"fmt"
)

func main() {
	t, _ := expirytable224.New(expirytable224.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable224.Batch{Now: 1, Ops: []expirytable224.Op{{Kind: expirytable224.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
