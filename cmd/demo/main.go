package main

import (
	"example.com/pairwise/quota/quota"
	"fmt"
	"log"
)

func main() {
	m, err := quota.New(quota.Options{MaxSubjects: 4, MaxReservations: 8, MaxMetadataBytes: 64, MaxNameBytes: 32})
	if err != nil {
		log.Fatal(err)
	}
	if err = m.SetLimits([]quota.Limit{{Subject: "team-a", Dimension: "cpu", Amount: 8}, {Subject: "team-a", Dimension: "memory", Amount: 32}}); err != nil {
		log.Fatal(err)
	}
	if err = m.Reserve("job-7", []quota.Demand{{Subject: "team-a", Dimension: "cpu", Amount: 2}, {Subject: "team-a", Dimension: "memory", Amount: 4}}, []byte("batch")); err != nil {
		log.Fatal(err)
	}
	s := m.Snapshot()
	fmt.Printf("generation=%d subjects=%d reservations=%d metadata=%d cpu-used=%d\n", s.Generation, s.Subjects, s.Reservations, s.MetadataBytes, s.SubjectState[0].Dimensions[0].Used)
}
