package main

import (
	"example.com/pairwise/expirytable454/expirytable454"
	"fmt"
)

func main() {
	t, _ := expirytable454.New(expirytable454.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable454.Batch{Now: 1, Ops: []expirytable454.Op{{Kind: expirytable454.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
