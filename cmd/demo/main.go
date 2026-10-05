package main

import (
	"example.com/pairwise/resourcelease114/resourcelease114"
	"fmt"
)

func main() {
	t, _ := resourcelease114.New(resourcelease114.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(resourcelease114.Batch{Now: 1, Ops: []resourcelease114.Op{{Kind: resourcelease114.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
