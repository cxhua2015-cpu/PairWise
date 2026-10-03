package quorumlog

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestBatchTemporaryOverflowThenRelease(t *testing.T) {
	q := mustNew(t, 5)
	mustOpen(t, q, "s", []string{"a"}, 1, 0)
	if _, err := q.Apply(ent("s", 1, "d1", "aaa", 0)); err != nil {
		t.Fatal(err)
	}
	// Adds 4 bytes (total 7 > 5 mid-batch) then commits both, releasing all.
	out, err := q.ApplyBatch([]Update{
		ent("s", 2, "d2", "bbbb", 1),
		ack("s", 1, "a", "d1", 1),
		ack("s", 2, "a", "d2", 1),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out[1].Committed) != 1 || len(out[2].Committed) != 1 {
		t.Fatalf("commit distribution: %+v", out)
	}
	if q.Snapshot().BufferedBytes != 0 {
		t.Fatal("bytes not released")
	}
}

func TestBatchFinalOverflowRollsBackEverything(t *testing.T) {
	q := mustNew(t, 3)
	mustOpen(t, q, "s", []string{"a"}, 1, 0)
	mustOpen(t, q, "t", []string{"a"}, 1, 0)
	before := q.Snapshot()
	out, err := q.ApplyBatch([]Update{
		ent("s", 1, "d", "aa", 1),
		seal("t", 0, 1), // would complete t; must be rolled back too
		ent("s", 2, "d", "aa", 1),
	})
	if !errors.Is(err, ErrCapacity) || out != nil {
		t.Fatalf("out=%v err=%v", out, err)
	}
	if !reflect.DeepEqual(before, q.Snapshot()) {
		t.Fatalf("state leaked: %+v", q.Snapshot())
	}
}

func TestErrorPrecedenceFirstErrorWins(t *testing.T) {
	q := mustNew(t, 100)
	mustOpen(t, q, "s", []string{"a"}, 1, 0)
	// Second update is also invalid, but the first error (time) must win.
	if _, err := q.Apply(ent("s", 1, "d", "x", 5)); err != nil {
		t.Fatal(err)
	}
	_, err := q.ApplyBatch([]Update{
		ent("s", 2, "d", "x", 4), // ErrTime
		{Stream: "s", At: 6},     // ErrInvalid
	})
	if !errors.Is(err, ErrTime) {
		t.Fatalf("precedence: %v", err)
	}
}

func TestExpiryBoundaryAndAccounting(t *testing.T) {
	q := mustNew(t, 100)
	mustOpen(t, q, "keep", []string{"a"}, 1, 5)
	mustOpen(t, q, "drop", []string{"a"}, 1, 4)
	if _, err := q.Apply(ent("drop", 1, "d", "zz", 4)); err != nil {
		t.Fatal(err)
	}
	exp, err := q.ExpireBefore(5)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(exp, []ExpiredStream{{Stream: "drop", Committed: 0, BufferedBytes: 2}}) {
		t.Fatalf("expired: %+v", exp)
	}
	// Equality retained: "keep" has LastActivity == 5.
	if len(q.Snapshot().Streams) != 1 || q.Snapshot().Streams[0].ID != "keep" {
		t.Fatalf("kept: %+v", q.Snapshot())
	}
	// Empty expiry returns non-nil empty slice.
	exp, err = q.ExpireBefore(0)
	if err != nil || exp == nil || len(exp) != 0 {
		t.Fatalf("empty expiry: %#v %v", exp, err)
	}
}

func TestOutcomePayloadIsolation(t *testing.T) {
	q := mustNew(t, 100)
	mustOpen(t, q, "s", []string{"a"}, 1, 0)
	in := []byte("abc")
	u := Update{Stream: "s", At: 0, Entry: &Entry{Index: 1, Digest: "d", Payload: in}}
	if _, err := q.Apply(u); err != nil {
		t.Fatal(err)
	}
	in[0] = 'X' // mutating input must not affect stored payload
	o, err := q.Apply(ack("s", 1, "a", "d", 1))
	if err != nil {
		t.Fatal(err)
	}
	if string(o.Committed[0].Payload) != "abc" {
		t.Fatalf("input alias leaked: %q", o.Committed[0].Payload)
	}
	o.Committed[0].Payload[0] = 'Y'
	// Re-observe via committed-digest idempotency: no state corruption.
	if _, err := q.Apply(ent("s", 1, "d", "abc", 2)); err != nil {
		t.Fatalf("post-commit duplicate: %v", err)
	}
}

func TestSealAndAckEdgeCases(t *testing.T) {
	q := mustNew(t, 100)
	mustOpen(t, q, "s", []string{"a", "b"}, 2, 0)
	if _, err := q.Apply(seal("s", 3, 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(seal("s", 4, 1)); !errors.Is(err, ErrConflict) {
		t.Fatalf("second different seal: %v", err)
	}
	// Mismatched ACK does not block matching quorum.
	if _, err := q.Apply(ent("s", 1, "d1", "x", 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(ack("s", 1, "a", "other", 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(ack("s", 1, "b", "d1", 1)); err != nil {
		t.Fatal(err)
	}
	o, err := q.Apply(ack("s", 1, "b", "d1", 1)) // duplicate is idempotent
	if err != nil || len(o.Committed) != 0 {
		t.Fatalf("dup ack: %+v %v", o, err)
	}
	// Only one matching voter so far; no commit yet.
	if q.Snapshot().Streams[0].Committed != 0 {
		t.Fatal("mismatched ack counted toward quorum")
	}
}

func TestConcurrentMixedLoad(t *testing.T) {
	q := mustNew(t, 1_000_000)
	const streams = 4
	const perStream = 50
	for i := 0; i < streams; i++ {
		mustOpen(t, q, fmt.Sprintf("s%d", i), []string{"a", "b", "c"}, 2, 0)
	}
	var wg sync.WaitGroup
	for i := 0; i < streams; i++ {
		id := fmt.Sprintf("s%d", i)
		for j := 1; j <= perStream; j++ {
			j := uint64(j)
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := q.ApplyBatch([]Update{
					ent(id, j, "d", "p", 1),
					ack(id, j, "a", "d", 1),
					ack(id, j, "b", "d", 1),
				})
				if err != nil {
					t.Error(err)
				}
			}()
		}
		wg.Add(2)
		go func() { defer wg.Done(); _ = q.Snapshot() }()
		go func() { defer wg.Done(); _, _ = q.ExpireBefore(0) }()
	}
	wg.Wait()
	s := q.Snapshot()
	if len(s.Streams) != streams || s.BufferedBytes != 0 {
		t.Fatalf("snapshot: %+v", s)
	}
	for _, st := range s.Streams {
		if st.Committed != perStream || st.PendingEntries != 0 || st.PendingAcks != 0 {
			t.Fatalf("stream %+v", st)
		}
	}
}

func TestConcurrentExpireAndApply(t *testing.T) {
	q := mustNew(t, 1_000_000)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := fmt.Sprintf("s%d", i)
			for j := 0; j < 100; j++ {
				_ = q.Open(StreamConfig{ID: id, Voters: []string{"a"}, Quorum: 1}, int64(j))
				_, _ = q.Apply(ent(id, 1, "d", "x", int64(j)))
				_, _ = q.ExpireBefore(int64(j))
			}
		}()
	}
	wg.Wait()
}
