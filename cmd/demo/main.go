package main

import (
	"fmt"
	"log"

	"example.com/pairwise/leasegraph/leasegraph"
)

func main() {
	s, err := leasegraph.New(leasegraph.Options{MaxTasks: 8, MaxBytes: 1024, LeaseDuration: 10, MaxAttempts: 2})
	if err != nil {
		log.Fatal(err)
	}
	err = s.AddBatch([]leasegraph.TaskSpec{
		{ID: "build", Priority: 1, Payload: []byte("src")},
		{ID: "test", Dependencies: []string{"build"}, Priority: 5},
	})
	if err != nil {
		log.Fatal(err)
	}
	l, err := s.Claim(5)
	if err != nil {
		log.Fatal(err)
	}
	tr, err := s.Complete(6, l.ID, l.Token, []byte("artifact"), true)
	if err != nil {
		log.Fatal(err)
	}
	snap := s.Snapshot()
	fmt.Printf("claimed=%s ready=%v tasks=%d bytes=%d\n", l.ID, tr.Ready, len(snap.Tasks), snap.UsedBytes)
}
