package main

import (
	"example.com/pairwise/resourcelease129/resourcelease129"
	"fmt"
)

func main() {
	t, _ := resourcelease129.New(resourcelease129.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(resourcelease129.Batch{Now: 1, Ops: []resourcelease129.Op{{Kind: resourcelease129.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
