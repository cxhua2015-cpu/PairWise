package main

import (
	"example.com/pairwise/tokenvault/tokenvault"
	"fmt"
)

func main() {
	t, _ := tokenvault.New(tokenvault.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(tokenvault.Batch{Now: 1, Ops: []tokenvault.Op{{Kind: tokenvault.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
