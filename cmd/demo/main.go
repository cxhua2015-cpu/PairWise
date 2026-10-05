package main

import (
	"example.com/pairwise/resourcelease099/resourcelease099"
	"fmt"
)

func main() {
	t, _ := resourcelease099.New(resourcelease099.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(resourcelease099.Batch{Now: 1, Ops: []resourcelease099.Op{{Kind: resourcelease099.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
