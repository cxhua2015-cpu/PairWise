package main

import (
	"example.com/pairwise/expirytable424/expirytable424"
	"fmt"
)

func main() {
	t, _ := expirytable424.New(expirytable424.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable424.Batch{Now: 1, Ops: []expirytable424.Op{{Kind: expirytable424.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
