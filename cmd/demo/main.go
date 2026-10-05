package main

import (
	"example.com/pairwise/heartbeat/heartbeat"
	"fmt"
)

func main() {
	t, _ := heartbeat.New(heartbeat.Options{MaxEntries: 4, MaxKeyBytes: 8})
	x, _ := t.Apply(heartbeat.Batch{Now: 1, Ops: []heartbeat.Op{{Kind: heartbeat.Put, Key: "a", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d entries=%d now=%d\n", x.Generation, x.Revision, len(t.Snapshot().Entries), t.Snapshot().Now)
}
