package main

import (
	"example.com/pairwise/imagecatalog/imagecatalog"
	"fmt"
)

func main() {
	s, _ := imagecatalog.New(imagecatalog.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(imagecatalog.Batch{Ops: []imagecatalog.Op{{Kind: imagecatalog.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
