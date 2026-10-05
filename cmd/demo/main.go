package main

import (
	"example.com/pairwise/expirytable249/expirytable249"
	"fmt"
)

func main() {
	t, _ := expirytable249.New(expirytable249.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(expirytable249.Batch{Now: 1, Ops: []expirytable249.Op{{Kind: expirytable249.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
