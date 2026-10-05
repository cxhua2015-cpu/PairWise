package main

import (
	"example.com/pairwise/expirytable394/expirytable394"
	"fmt"
)

func main() {
	t, _ := expirytable394.New(expirytable394.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable394.Batch{Now: 1, Ops: []expirytable394.Op{{Kind: expirytable394.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
