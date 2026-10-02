package main

import (
	"fmt"
	"log"

	"example.com/pairwise/watermarkjoin/join"
)

func event(side join.Side, key, id string, at int64, payload string) join.Update {
	e := join.Event{Side: side, Key: key, ID: id, Time: at, Payload: []byte(payload)}
	return join.Update{Event: &e}
}

func watermark(side join.Side, at int64) join.Update {
	w := join.Watermark{Side: side, Time: at}
	return join.Update{Watermark: &w}
}

func main() {
	j, err := join.New(join.Options{Window: 3, MaxEvents: 8, MaxBytes: 128})
	if err != nil {
		log.Fatal(err)
	}
	out, err := j.ApplyBatch([]join.Update{
		event(join.Right, "device-7", "r1", 12, "warm"),
		event(join.Left, "device-7", "l1", 10, "alert"),
		watermark(join.Left, 16),
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("matches=%d pair=%s/%s expired=%d remaining=%d\n",
		len(out[1].Matches), out[1].Matches[0].LeftID, out[1].Matches[0].RightID,
		len(out[2].Expired), j.Snapshot().Count)
}
