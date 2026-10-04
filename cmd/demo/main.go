package main

import (
	"example.com/pairwise/casstore/casstore"
	"fmt"
	"log"
)

func main() {
	s, e := casstore.New(casstore.Options{MaxKeys: 8, MaxValueBytes: 64, MaxNameBytes: 32})
	if e != nil {
		log.Fatal(e)
	}
	r, e := s.Transact(casstore.Txn{Compares: []casstore.Compare{{Kind: casstore.NotExists, Key: "cfg/a"}}, Writes: []casstore.Write{{Kind: casstore.Put, Key: "cfg/a", Value: []byte("one")}, {Kind: casstore.Put, Key: "cfg/b", Value: []byte("two")}}})
	if e != nil {
		log.Fatal(e)
	}
	items, e := s.List("cfg/", "", 10)
	if e != nil {
		log.Fatal(e)
	}
	fmt.Printf("succeeded=%t generation=%d revision=%d keys=%d first=%s\n", r.Succeeded, r.Generation, r.Revision, len(items), items[0].Key)
}
