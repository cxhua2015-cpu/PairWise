package main

import (
	"example.com/pairwise/hashring/hashring"
	"fmt"
	"log"
)

func main() {
	r, e := hashring.New(hashring.Options{MaxNodes: 8, MaxTokens: 64, MaxValueBytes: 1024})
	if e != nil {
		log.Fatal(e)
	}
	_, e = r.ApplyBatch([]hashring.Change{{Type: hashring.ChangeAdd, Node: hashring.Node{ID: "a", Weight: 2, Value: []byte("A")}}, {Type: hashring.ChangeAdd, Node: hashring.Node{ID: "b", Weight: 1, Value: []byte("B")}}})
	if e != nil {
		log.Fatal(e)
	}
	o, g, e := r.Lookup([]byte("customer-42"), 2)
	if e != nil {
		log.Fatal(e)
	}
	fmt.Printf("generation=%d owners=%s,%s tokens=%d\n", g, o[0].ID, o[1].ID, len(r.Snapshot().Tokens))
}
