package main

import (
	"fmt"
	"log"

	"example.com/pairwise/quorumlog/quorumlog"
)

func entry(index uint64, digest, payload string) quorumlog.Update {
	e := quorumlog.Entry{Index: index, Digest: digest, Payload: []byte(payload)}
	return quorumlog.Update{Stream: "orders", At: 1, Entry: &e}
}

func ack(index uint64, replica, digest string) quorumlog.Update {
	a := quorumlog.Ack{Index: index, Replica: replica, Digest: digest}
	return quorumlog.Update{Stream: "orders", At: 1, Ack: &a}
}

func main() {
	q, err := quorumlog.New(quorumlog.Options{MaxBufferedBytes: 64})
	if err != nil {
		log.Fatal(err)
	}
	if err := q.Open(quorumlog.StreamConfig{
		ID: "orders", Voters: []string{"r3", "r1", "r2"}, Quorum: 2,
	}, 0); err != nil {
		log.Fatal(err)
	}
	last := quorumlog.Seal{LastIndex: 2}
	out, err := q.ApplyBatch([]quorumlog.Update{
		ack(2, "r1", "d2"),
		ack(2, "r2", "d2"),
		entry(2, "d2", "second"),
		entry(1, "d1", "first"),
		ack(1, "r1", "d1"),
		ack(1, "r2", "d1"),
		{Stream: "orders", At: 2, Seal: &last},
	})
	if err != nil {
		log.Fatal(err)
	}
	committed := out[5].Committed
	fmt.Printf("committed=%d first=%d last=%d completed=%t buffered=%d\n",
		len(committed), committed[0].Index, committed[len(committed)-1].Index,
		out[6].Completed, q.Snapshot().BufferedBytes)
}
