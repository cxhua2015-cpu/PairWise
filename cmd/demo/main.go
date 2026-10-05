package main

import (
	"example.com/pairwise/locklease/locklease"
	"fmt"
)

func main() {
	t, _ := locklease.New(locklease.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(locklease.Batch{Now: 1, Ops: []locklease.Op{{Kind: locklease.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
