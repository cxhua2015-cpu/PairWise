package main

import (
	"example.com/pairwise/resourcelease089/resourcelease089"
	"fmt"
)

func main() {
	t, _ := resourcelease089.New(resourcelease089.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(resourcelease089.Batch{Now: 1, Ops: []resourcelease089.Op{{Kind: resourcelease089.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
