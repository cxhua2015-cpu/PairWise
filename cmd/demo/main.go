package main

import (
	"example.com/pairwise/expirytable469/expirytable469"
	"fmt"
)

func main() {
	t, _ := expirytable469.New(expirytable469.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable469.Batch{Now: 1, Ops: []expirytable469.Op{{Kind: expirytable469.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
