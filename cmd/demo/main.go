package main

import (
	"example.com/pairwise/windowlimit/windowlimit"
	"fmt"
)

func main() {
	l, _ := windowlimit.New(windowlimit.Options{Window: 10, Limit: 5, MaxKeys: 8, MaxKeyBytes: 16, MaxEventsPerKey: 8})
	x, _ := l.Check(windowlimit.Batch{Now: 1, Requests: []windowlimit.Request{{Key: "api", Units: 3}, {Key: "api", Units: 3}}})
	fmt.Printf("generation=%d revision=%d allowed=%t/%t used=%d keys=%d\n", x.Generation, x.Revision, x.Decisions[0].Allowed, x.Decisions[1].Allowed, x.Decisions[1].Used, len(l.Snapshot().Keys))
}
