package main

import (
	"example.com/pairwise/resourcelease104/resourcelease104"
	"fmt"
)

func main() {
	t, _ := resourcelease104.New(resourcelease104.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(resourcelease104.Batch{Now: 1, Ops: []resourcelease104.Op{{Kind: resourcelease104.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
