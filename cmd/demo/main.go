package main

import (
	"example.com/pairwise/resourcelease149/resourcelease149"
	"fmt"
)

func main() {
	t, _ := resourcelease149.New(resourcelease149.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(resourcelease149.Batch{Now: 1, Ops: []resourcelease149.Op{{Kind: resourcelease149.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
