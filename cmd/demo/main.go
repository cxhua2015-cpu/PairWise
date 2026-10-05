package main

import (
	"example.com/pairwise/resourcelease169/resourcelease169"
	"fmt"
)

func main() {
	t, _ := resourcelease169.New(resourcelease169.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(resourcelease169.Batch{Now: 1, Ops: []resourcelease169.Op{{Kind: resourcelease169.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
