package main

import (
	"example.com/pairwise/credentiallease/credentiallease"
	"fmt"
)

func main() {
	t, _ := credentiallease.New(credentiallease.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(credentiallease.Batch{Now: 1, Ops: []credentiallease.Op{{Kind: credentiallease.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
