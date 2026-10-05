package main

import (
	"example.com/pairwise/expirytable324/expirytable324"
	"fmt"
)

func main() {
	t, _ := expirytable324.New(expirytable324.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable324.Batch{Now: 1, Ops: []expirytable324.Op{{Kind: expirytable324.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
