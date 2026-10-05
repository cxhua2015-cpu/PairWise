package main

import (
	"example.com/pairwise/expirytable399/expirytable399"
	"fmt"
)

func main() {
	t, _ := expirytable399.New(expirytable399.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable399.Batch{Now: 1, Ops: []expirytable399.Op{{Kind: expirytable399.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
