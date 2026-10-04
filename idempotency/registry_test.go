package idempotency

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestTakeoverInvalidatesOldToken(t *testing.T) {
	r := newRegistry(t, Options{MaxEntries: 4, MaxResultBytes: 16, MaxKeyBytes: 16})
	a, _ := r.Begin("k", "f1", 0, 5)
	b, err := r.Begin("k", "f2", 5, 5)
	if err != nil || !b.Leader {
		t.Fatalf("takeover=%+v err=%v", b, err)
	}
	if err := r.Renew("k", a.Token, 6, 5); !errors.Is(err, ErrStaleToken) {
		t.Fatalf("old renew=%v", err)
	}
	if err := r.Abort("k", a.Token); !errors.Is(err, ErrStaleToken) {
		t.Fatalf("old abort=%v", err)
	}
	if err := r.Complete("k", a.Token, []byte("x"), 6, 5); !errors.Is(err, ErrStaleToken) {
		t.Fatalf("old complete=%v", err)
	}
	if err := r.Renew("k", b.Token, 6, 5); err != nil {
		t.Fatalf("new renew=%v", err)
	}
	if got := r.Snapshot().Records[0].LeaseUntil; got != 11 {
		t.Fatalf("lease=%d", got)
	}
}

func TestCompletedRecordRejectsLeaderOps(t *testing.T) {
	r := newRegistry(t, Options{MaxEntries: 4, MaxResultBytes: 16, MaxKeyBytes: 16})
	b, _ := r.Begin("k", "f", 0, 10)
	if err := r.Complete("k", b.Token, []byte("v"), 1, 10); err != nil {
		t.Fatal(err)
	}
	if err := r.Renew("k", b.Token, 2, 5); !errors.Is(err, ErrStaleToken) {
		t.Fatalf("renew=%v", err)
	}
	if err := r.Abort("k", b.Token); !errors.Is(err, ErrStaleToken) {
		t.Fatalf("abort=%v", err)
	}
	if err := r.Complete("k", b.Token, []byte("v"), 2, 5); !errors.Is(err, ErrStaleToken) {
		t.Fatalf("complete=%v", err)
	}
}

func TestTimeBoundaries(t *testing.T) {
	r := newRegistry(t, Options{MaxEntries: 4, MaxResultBytes: 16, MaxKeyBytes: 16})
	b, _ := r.Begin("k", "f", 10, 5) // lease until 15
	if err := r.Renew("k", b.Token, 15, 1); !errors.Is(err, ErrStaleToken) {
		t.Fatalf("renew at deadline=%v", err)
	}
	if err := r.Complete("k", b.Token, []byte("x"), 15, 1); !errors.Is(err, ErrStaleToken) {
		t.Fatalf("complete at deadline=%v", err)
	}
	if err := r.Renew("k", b.Token, 14, 1); err != nil {
		t.Fatalf("renew before deadline=%v", err)
	}
	if _, err := r.Begin("k", "f", 0, 0); !errors.Is(err, ErrInvalidTime) {
		t.Fatalf("zero lease=%v", err)
	}
	if _, err := r.Begin("k", "f", math.MaxInt64-1, 2); !errors.Is(err, ErrInvalidTime) {
		t.Fatalf("overflow begin=%v", err)
	}
	if err := r.Renew("k", b.Token, math.MaxInt64-1, 2); !errors.Is(err, ErrInvalidTime) {
		t.Fatalf("overflow renew=%v", err)
	}
	if err := r.Complete("k", b.Token, []byte("x"), 14, math.MaxInt64); !errors.Is(err, ErrInvalidTime) {
		t.Fatalf("overflow complete=%v", err)
	}
	if _, err := r.Sweep(-1, 0); !errors.Is(err, ErrInvalidTime) {
		t.Fatalf("sweep now=%v", err)
	}
	if _, err := r.Sweep(0, -1); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("sweep limit=%v", err)
	}
}

func TestSweepBoundariesAndCompletedTTL(t *testing.T) {
	r := newRegistry(t, Options{MaxEntries: 4, MaxResultBytes: 16, MaxKeyBytes: 16})
	b, _ := r.Begin("done", "f", 0, 5)
	if err := r.Complete("done", b.Token, []byte("xy"), 1, 9); err != nil { // replay until 10
		t.Fatal(err)
	}
	if _, err := r.Begin("pend", "f", 0, 10); err != nil {
		t.Fatal(err)
	}
	// At now=9: completed still replayable, pending not yet expired.
	keys, err := r.Sweep(9, 0)
	if err != nil || len(keys) != 0 {
		t.Fatalf("sweep9=%v err=%v", keys, err)
	}
	if got := r.Snapshot().Generation; got != 3 {
		t.Fatalf("empty sweep bumped generation=%d", got)
	}
	// At now=10: both expire (now >= deadline).
	keys, _ = r.Sweep(10, 0)
	if len(keys) != 2 || keys[0] != "done" || keys[1] != "pend" {
		t.Fatalf("sweep10=%v", keys)
	}
	s := r.Snapshot()
	if s.Entries != 0 || s.ResultBytes != 0 || s.Generation != 4 {
		t.Fatalf("snapshot=%+v", s)
	}
}

func TestCapacityAndResultBytesAccounting(t *testing.T) {
	r := newRegistry(t, Options{MaxEntries: 2, MaxResultBytes: 4, MaxKeyBytes: 16})
	if _, err := r.Begin("a", "f", 0, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Begin("b", "f", 0, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Begin("c", "f", 0, 10); !errors.Is(err, ErrCapacity) {
		t.Fatalf("capacity=%v", err)
	}
	// Replacing an existing (expired) record does not consume a new slot.
	na, err := r.Begin("a", "g", 10, 5)
	if err != nil {
		t.Fatalf("replace=%v", err)
	}
	if err := r.Complete("a", na.Token, []byte("1234"), 11, 5); err != nil {
		t.Fatal(err)
	}
	if got := r.Snapshot().ResultBytes; got != 4 {
		t.Fatalf("resultBytes=%d", got)
	}
	// Aborting a pending entry frees its slot.
	b := r.Snapshot().Records[1]
	if err := r.Abort(b.Key, b.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Begin("c", "f", 0, 10); err != nil {
		t.Fatalf("after abort=%v", err)
	}
	// Sweeping the completed entry releases its result bytes.
	if _, err := r.Sweep(16, 0); err != nil {
		t.Fatal(err)
	}
	if got := r.Snapshot().ResultBytes; got != 0 {
		t.Fatalf("resultBytes after sweep=%d", got)
	}
}

func TestFailedOpsDoNotConsumeTokens(t *testing.T) {
	r := newRegistry(t, Options{MaxEntries: 1, MaxResultBytes: 2, MaxKeyBytes: 16})
	a, _ := r.Begin("a", "f", 0, 100)
	if _, err := r.Begin("b", "f", 0, 1); !errors.Is(err, ErrCapacity) {
		t.Fatalf("capacity=%v", err)
	}
	if err := r.Abort("a", a.Token); err != nil {
		t.Fatal(err)
	}
	b, err := r.Begin("b", "f", 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if b.Token != a.Token+1 {
		t.Fatalf("token gap: a=%d b=%d", a.Token, b.Token)
	}
}

func TestConcurrentMixedOperations(t *testing.T) {
	r := newRegistry(t, Options{MaxEntries: 64, MaxResultBytes: 32, MaxKeyBytes: 16})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			key := string(rune('a'+id%4)) + string(rune('0'+id/4))
			for now := int64(0); now < 50; now++ {
				res, err := r.Begin(key, "fp", now, 3)
				if err != nil {
					continue
				}
				if res.Leader {
					_ = r.Renew(key, res.Token, now, 5)
					_ = r.Complete(key, res.Token, []byte("r"), now, 4)
				}
				_, _ = r.Sweep(now, 1)
				_ = r.Snapshot()
			}
		}(i)
	}
	wg.Wait()
	s := r.Snapshot()
	if s.Entries != len(s.Records) {
		t.Fatalf("entries=%d records=%d", s.Entries, len(s.Records))
	}
	total := 0
	for _, rec := range s.Records {
		if rec.Pending && rec.Result != nil {
			t.Fatalf("pending with result: %+v", rec)
		}
		total += len(rec.Result)
	}
	if total != s.ResultBytes {
		t.Fatalf("resultBytes=%d want=%d", s.ResultBytes, total)
	}
}
