package main

import (
	"example.com/pairwise/resourcelease134/resourcelease134"
	"fmt"
)

func main() {
	t, _ := resourcelease134.New(resourcelease134.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(resourcelease134.Batch{Now: 1, Ops: []resourcelease134.Op{{Kind: resourcelease134.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
