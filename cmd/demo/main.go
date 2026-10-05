package main

import (
	"example.com/pairwise/resourcelease119/resourcelease119"
	"fmt"
)

func main() {
	t, _ := resourcelease119.New(resourcelease119.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(resourcelease119.Batch{Now: 1, Ops: []resourcelease119.Op{{Kind: resourcelease119.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
