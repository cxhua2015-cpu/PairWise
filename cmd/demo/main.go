package main

import (
	"example.com/pairwise/resourcelease109/resourcelease109"
	"fmt"
)

func main() {
	t, _ := resourcelease109.New(resourcelease109.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(resourcelease109.Batch{Now: 1, Ops: []resourcelease109.Op{{Kind: resourcelease109.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
