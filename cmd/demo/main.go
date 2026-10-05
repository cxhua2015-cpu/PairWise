package main

import (
	"example.com/pairwise/devicelease/devicelease"
	"fmt"
)

func main() {
	t, _ := devicelease.New(devicelease.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(devicelease.Batch{Now: 1, Ops: []devicelease.Op{{Kind: devicelease.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
