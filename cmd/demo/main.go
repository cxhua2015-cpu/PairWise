package main

import (
	"example.com/pairwise/leasepool/leasepool"
	"fmt"
)

func main() {
	p, _ := leasepool.New(leasepool.Options{MaxResources: 4, MaxNameBytes: 16, MaxOwnerBytes: 16})
	x, _ := p.Apply(leasepool.Batch{Now: 1, Ops: []leasepool.Op{{Kind: leasepool.Add, Resource: "gpu"}, {Kind: leasepool.Acquire, Resource: "gpu", Owner: "worker", ExpiresAt: 5}}})
	fmt.Printf("generation=%d revision=%d resources=%d leases=%d owner=%s\n", x.Generation, x.Revision, len(p.Snapshot().Resources), len(p.Snapshot().Leases), p.Snapshot().Leases[0].Owner)
}
