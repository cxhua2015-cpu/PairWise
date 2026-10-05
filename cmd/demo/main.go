package main

import (
	"example.com/pairwise/resourcelease144/resourcelease144"
	"fmt"
)

func main() {
	t, _ := resourcelease144.New(resourcelease144.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(resourcelease144.Batch{Now: 1, Ops: []resourcelease144.Op{{Kind: resourcelease144.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
