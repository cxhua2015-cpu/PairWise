package main

import (
	"example.com/pairwise/expirytable244/expirytable244"
	"fmt"
)

func main() {
	t, _ := expirytable244.New(expirytable244.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable244.Batch{Now: 1, Ops: []expirytable244.Op{{Kind: expirytable244.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
