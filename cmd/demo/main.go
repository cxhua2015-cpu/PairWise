package main

import (
	"example.com/pairwise/dedupcache/dedupcache"
	"fmt"
)

func main() {
	c, _ := dedupcache.New(dedupcache.Options{MaxEntries: 4, MaxKeyBytes: 16, MaxTokenBytes: 16, MaxValueBytes: 16, MaxTotalValueBytes: 32})
	x, _ := c.Apply(dedupcache.Batch{Now: 1, Ops: []dedupcache.Op{{Kind: dedupcache.Put, Key: "pay/1", Token: "req-1", Value: []byte("ok"), ExpiresAt: 9}, {Kind: dedupcache.Put, Key: "pay/1", Token: "req-1", Value: []byte("ignored"), ExpiresAt: 10}}})
	fmt.Printf("generation=%d revision=%d created=%t replay=%t value=%s records=%d\n", x.Generation, x.Revision, x.Outcomes[0].Created, x.Outcomes[1].Replay, x.Outcomes[1].Record.Value, len(c.Snapshot().Records))
}
