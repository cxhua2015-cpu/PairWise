package main

import (
	"example.com/pairwise/resourcelease174/resourcelease174"
	"fmt"
)

func main() {
	t, _ := resourcelease174.New(resourcelease174.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(resourcelease174.Batch{Now: 1, Ops: []resourcelease174.Op{{Kind: resourcelease174.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
