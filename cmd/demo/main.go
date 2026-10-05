package main

import (
	"example.com/pairwise/sessiontable/sessiontable"
	"fmt"
)

func main() {
	t, _ := sessiontable.New(sessiontable.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(sessiontable.Batch{Now: 1, Ops: []sessiontable.Op{{Kind: sessiontable.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
