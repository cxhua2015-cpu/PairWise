package main

import (
	"example.com/pairwise/resourcelease139/resourcelease139"
	"fmt"
)

func main() {
	t, _ := resourcelease139.New(resourcelease139.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(resourcelease139.Batch{Now: 1, Ops: []resourcelease139.Op{{Kind: resourcelease139.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
