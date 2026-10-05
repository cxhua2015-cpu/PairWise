package main

import (
	"example.com/pairwise/configstack/configstack"
	"fmt"
)

func main() {
	s, _ := configstack.New(configstack.Options{MaxLayers: 4, MaxEntries: 8, MaxNameBytes: 16, MaxKeyBytes: 16, MaxValueBytes: 16, MaxTotalValueBytes: 64})
	x, _ := s.Apply(configstack.Batch{Ops: []configstack.Op{{Kind: configstack.AddLayer, Name: "base"}, {Kind: configstack.Set, Name: "base", Key: "color", Value: []byte("blue")}}})
	e, f, _ := s.Resolve("color")
	fmt.Printf("generation=%d revision=%d layers=%d found=%t value=%s\n", x.Generation, x.Revision, len(s.Snapshot().Layers), f, e.Value)
}
