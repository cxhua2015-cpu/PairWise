package main

import (
	"example.com/pairwise/reservation/reservation"
	"fmt"
	"log"
)

func main() {
	l, e := reservation.New(reservation.Options{MaxResources: 4, MaxReservations: 16, MaxValueBytes: 1024})
	if e != nil {
		log.Fatal(e)
	}
	g, e := l.ApplyBatch([]reservation.Change{{Type: reservation.ChangeAdd, Reservation: reservation.Reservation{ID: "job-1", Resource: "gpu-1", Start: 10, End: 20, Value: []byte("train")}}})
	if e != nil {
		log.Fatal(e)
	}
	r, e := l.At("gpu-1", 10)
	if e != nil {
		log.Fatal(e)
	}
	fmt.Printf("generation=%d found=%t id=%s resources=%d\n", g, r.Found, r.Reservation.ID, len(l.Snapshot().Resources))
}
