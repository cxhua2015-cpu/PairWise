package main

import (
	"example.com/pairwise/resourcelease194/resourcelease194"
	"fmt"
)

func main() {
	t, _ := resourcelease194.New(resourcelease194.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(resourcelease194.Batch{Now: 1, Ops: []resourcelease194.Op{{Kind: resourcelease194.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
