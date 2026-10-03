package main

import (
	"example.com/pairwise/topiclog/topiclog"
	"fmt"
	"log"
)

func main() {
	l, err := topiclog.New(topiclog.Options{Partitions: 2, MaxTopics: 4, MaxRecords: 16, MaxPayloadBytes: 1024})
	if err != nil {
		log.Fatal(err)
	}
	recs, gen, err := l.AppendBatch([]topiclog.Input{{Topic: "orders", Key: "customer-1", Payload: []byte("created")}})
	if err != nil {
		log.Fatal(err)
	}
	if _, err = l.CommitBatch([]topiclog.Commit{{Group: "billing", Topic: "orders", Partition: recs[0].Partition, Offset: recs[0].Offset}}); err != nil {
		log.Fatal(err)
	}
	if _, err = l.Trim("orders", recs[0].Partition, recs[0].Offset); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("generation=%d partition=%d offset=%d remaining=%d\n", gen, recs[0].Partition, recs[0].Offset, l.Snapshot().UsedRecords)
}
