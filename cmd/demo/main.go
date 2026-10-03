package main

import (
	"fmt"
	"log"

	"example.com/pairwise/mvccwatch/mvcc"
)

func main() {
	s, err := mvcc.New(mvcc.Options{MaxLiveBytes: 128})
	if err != nil {
		log.Fatal(err)
	}
	if _, err = s.Put("cfg/a", []byte("v1")); err != nil {
		log.Fatal(err)
	}
	r, err := s.Txn(
		[]mvcc.Compare{{Key: "cfg/a", Target: mvcc.CompareVersion, Result: mvcc.CompareEqual, Revision: 1}},
		[]mvcc.Op{{Type: mvcc.OpPut, Key: "cfg/a", Value: []byte("v2")}, {Type: mvcc.OpPut, Key: "cfg/b", Value: []byte("on")}},
		nil,
	)
	if err != nil {
		log.Fatal(err)
	}
	events, err := s.Watch("cfg/", 0, 0)
	if err != nil {
		log.Fatal(err)
	}
	snap := s.Snapshot()
	fmt.Printf("succeeded=%t revision=%d events=%d keys=%d live=%d\n", r.Succeeded, snap.Revision, len(events), len(snap.KVs), snap.LiveBytes)
}
