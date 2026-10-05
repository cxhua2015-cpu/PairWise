package main

import (
	"example.com/pairwise/resourcelease094/resourcelease094"
	"fmt"
)

func main() {
	t, _ := resourcelease094.New(resourcelease094.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(resourcelease094.Batch{Now: 1, Ops: []resourcelease094.Op{{Kind: resourcelease094.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
