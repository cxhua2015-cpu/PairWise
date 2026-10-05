package main

import (
	"example.com/pairwise/resourcelease084/resourcelease084"
	"fmt"
)

func main() {
	t, _ := resourcelease084.New(resourcelease084.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(resourcelease084.Batch{Now: 1, Ops: []resourcelease084.Op{{Kind: resourcelease084.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
