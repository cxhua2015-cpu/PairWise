package main

import (
	"example.com/pairwise/prefixacl/prefixacl"
	"fmt"
	"net/netip"
)

func main() {
	t, _ := prefixacl.New(prefixacl.Options{MaxRules: 8, Default: prefixacl.Deny})
	x, _ := t.Apply(prefixacl.Batch{Ops: []prefixacl.Op{{Kind: prefixacl.Upsert, Prefix: netip.MustParsePrefix("10.0.0.0/8"), Action: prefixacl.Allow}}})
	a, p, f, _ := t.Lookup(netip.MustParseAddr("10.2.3.4"))
	fmt.Printf("generation=%d revision=%d rules=%d found=%t action=%d prefix=%s\n", x.Generation, x.Revision, len(t.Snapshot().Rules), f, a, p)
}
