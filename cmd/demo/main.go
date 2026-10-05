package main

import (
	"example.com/pairwise/resourcelease164/resourcelease164"
	"fmt"
)

func main() {
	t, _ := resourcelease164.New(resourcelease164.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(resourcelease164.Batch{Now: 1, Ops: []resourcelease164.Op{{Kind: resourcelease164.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
