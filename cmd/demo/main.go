package main

import (
	"example.com/pairwise/expirytable289/expirytable289"
	"fmt"
)

func main() {
	t, _ := expirytable289.New(expirytable289.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable289.Batch{Now: 1, Ops: []expirytable289.Op{{Kind: expirytable289.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
