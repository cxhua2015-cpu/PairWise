package main

import (
	"example.com/pairwise/certificatelease/certificatelease"
	"fmt"
)

func main() {
	t, _ := certificatelease.New(certificatelease.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(certificatelease.Batch{Now: 1, Ops: []certificatelease.Op{{Kind: certificatelease.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
