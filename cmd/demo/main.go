package main

import (
	"example.com/pairwise/rankboard/rankboard"
	"fmt"
)

func main() {
	b, _ := rankboard.New(rankboard.Options{MaxItems: 8, MaxIDBytes: 16, MaxAbsScore: 100})
	x, _ := b.Apply(rankboard.Batch{Ops: []rankboard.Op{{Kind: rankboard.Add, ID: "alice", Delta: 7}, {Kind: rankboard.Add, ID: "bob", Delta: 9}}})
	top, _ := b.Top(1)
	fmt.Printf("generation=%d revision=%d items=%d leader=%s score=%d\n", x.Generation, x.Revision, len(b.Snapshot().Items), top[0].ID, top[0].Score)
}
