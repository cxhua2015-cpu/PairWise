package idempotency

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestTakeoverInvalidatesOldTokenEverywhere(t *testing.T) {
	r := newRegistry(t, Options{MaxEntries: 2, MaxResultBytes: 10, MaxKeyBytes: 10})
	a, _ := r.Begin("k", "f1", 0, 5)
	b, err := r.Begin("k", "f2", 5, 5)
	if err != nil || !b.Leader {
		t.Fatalf("takeover: %+v %v", b, err)
	}
	if err := r.Renew("k", a.Token, 6, 5); !errors.Is(err, ErrStaleToken) {
		t.Fatalf("old renew: %v", err)
	}
	if err := r.Complete("k", a.Token, []byte("x"), 6, 5); !errors.Is(err, ErrStaleToken) {
		t.Fatalf("old complete: %v", err)
	}
	if err := r.Abort("k", a.Token); !errors.Is(err, ErrStaleToken) {
		t.Fatalf("old abort: %v", err)
	}
	if err := r.Renew("k", b.Token, 6, 5); err != nil {
		t.Fatalf("new renew: %v", err)
	}
	s := r.Snapshot()
	if s.Entries != 1 || s.Records[0].LeaseUntil != 11 || s.Records[0].Token != b.Token {
		t.Fatalf("snapshot: %+v", s)
	}
}

func TestBoundaryTimes(t *testing.T) {
	r := newRegistry(t, Options{MaxEntries: 3, MaxResultBytes: 10, MaxKeyBytes: 10})
	b, _ := r.Begin("k", "f", 0, 5)
	// now == LeaseUntil: renew/complete are stale, begin takes over.
	if err := r.Renew("k", b.Token, 5, 1); !errors.Is(err, ErrStaleToken) {
		t.Fatalf("renew at deadline: %v", err)
	}
	if err := r.Complete("k", b.Token, nil, 5, 1); !errors.Is(err, ErrStaleToken) {
		t.Fatalf("complete at deadline: %v", err)
	}
	if err := r.Renew("k", b.Token, 4, 10); err != nil {
		t.Fatal(err)
	}
	if err := r.Complete("k", b.Token, []byte("r"), 13, 5); err != nil {
		t.Fatal(err)
	}
	// replay valid until ReplayUntil=18; at 18 record behaves as absent.
	rep, err := r.Begin("k", "f", 17, 1)
	if err != nil || !rep.Replay {
		t.Fatalf("replay before expiry: %+v %v", rep, err)
	}
	n, err := r.Begin("k", "f", 18, 1)
	if err != nil || !n.Leader {
		t.Fatalf("begin at replay expiry: %+v %v", n, err)
	}
	// sweep boundary: pending LeaseUntil=19, not expired at 18.
	keys, _ := r.Sweep(18, 0)
	if len(keys) != 0 {
		t.Fatalf("sweep before expiry: %v", keys)
	}
	keys, _ = r.Sweep(19, 0)
	if len(keys) != 1 || keys[0] != "k" {
		t.Fatalf("sweep at expiry: %v", keys)
	}
	if g := r.Snapshot().Generation; g != 5 {
		t.Fatalf("generation=%d", g)
	}
}

func TestInvalidArgsAndOverflow(t *testing.T) {
	r := newRegistry(t, Options{MaxEntries: 2, MaxResultBytes: 4, MaxKeyBytes: 4})
	b, _ := r.Begin("k", "f", 0, 100)
	gen := r.Snapshot().Generation
	if err := r.Renew("k", 0, 0, 1); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("zero token: %v", err)
	}
	if err := r.Renew("k", b.Token, 0, 0); !errors.Is(err, ErrInvalidTime) {
		t.Fatalf("zero lease: %v", err)
	}
	if err := r.Renew("k", b.Token, math.MaxInt64, 1); !errors.Is(err, ErrInvalidTime) {
		t.Fatalf("renew overflow: %v", err)
	}
	if err := r.Complete("k", b.Token, nil, math.MaxInt64-1, 2); !errors.Is(err, ErrInvalidTime) {
		t.Fatalf("complete overflow: %v", err)
	}
	if err := r.Complete("k", b.Token, []byte("12345"), 1, 2); !errors.Is(err, ErrResultTooLarge) {
		t.Fatalf("too large: %v", err)
	}
	if _, err := r.Sweep(-1, 0); !errors.Is(err, ErrInvalidTime) {
		t.Fatalf("sweep now: %v", err)
	}
	if _, err := r.Sweep(0, -1); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("sweep limit: %v", err)
	}
	if g := r.Snapshot().Generation; g != gen {
		t.Fatalf("failed ops changed generation: %d != %d", g, gen)
	}
}

func TestCapacityResultBytesAndReplacement(t *testing.T) {
	r := newRegistry(t, Options{MaxEntries: 2, MaxResultBytes: 4, MaxKeyBytes: 10})
	a, _ := r.Begin("a", "f", 0, 10)
	if err := r.Complete("a", a.Token, []byte("1234"), 1, 100); err != nil {
		t.Fatal(err)
	}
	b, _ := r.Begin("b", "f", 0, 10)
	// entries full -> new key rejected even though result bytes would fit
	if _, err := r.Begin("c", "f", 0, 10); !errors.Is(err, ErrCapacity) {
		t.Fatalf("entries capacity: %v", err)
	}
	// unexpired completed record with different fingerprint conflicts
	if _, err := r.Begin("a", "g", 0, 10); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflict: %v", err)
	}
	// same fingerprint replays
	rep, err := r.Begin("a", "f", 2, 10)
	if err != nil || !rep.Replay || string(rep.Result) != "1234" {
		t.Fatalf("replay: %+v %v", rep, err)
	}
	_ = b
}

func TestCapacityReplacementOfCompletedFreesBytes(t *testing.T) {
	r := newRegistry(t, Options{MaxEntries: 1, MaxResultBytes: 4, MaxKeyBytes: 10})
	a, _ := r.Begin("a", "f", 0, 10)
	if err := r.Complete("a", a.Token, []byte("1234"), 1, 5); err != nil {
		t.Fatal(err)
	}
	// replay expired at 6; same key replacement must fit capacity
	n, err := r.Begin("a", "f", 6, 10)
	if err != nil || !n.Leader {
		t.Fatalf("replacement: %+v %v", n, err)
	}
	s := r.Snapshot()
	if s.Entries != 1 || s.ResultBytes != 0 {
		t.Fatalf("snapshot: %+v", s)
	}
}

func TestOwnershipCompletedRecordRejectsAllTokens(t *testing.T) {
	r := newRegistry(t, Options{MaxEntries: 2, MaxResultBytes: 10, MaxKeyBytes: 10})
	b, _ := r.Begin("k", "f", 0, 10)
	if err := r.Complete("k", b.Token, []byte("v"), 1, 50); err != nil {
		t.Fatal(err)
	}
	if err := r.Renew("k", b.Token, 2, 5); !errors.Is(err, ErrStaleToken) {
		t.Fatalf("renew completed: %v", err)
	}
	if err := r.Abort("k", b.Token); !errors.Is(err, ErrStaleToken) {
		t.Fatalf("abort completed: %v", err)
	}
	if err := r.Abort("missing", b.Token); !errors.Is(err, ErrStaleToken) {
		t.Fatalf("abort missing: %v", err)
	}
	if g := r.Snapshot().Generation; g != 2 {
		t.Fatalf("generation=%d", g)
	}
}

func TestConcurrentMixedOperations(t *testing.T) {
	r := newRegistry(t, Options{MaxEntries: 8, MaxResultBytes: 64, MaxKeyBytes: 16})
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := string(rune('a' + i%4))
			res, err := r.Begin(key, "f", 0, 100)
			if err != nil {
				return
			}
			if res.Leader {
				_ = r.Renew(key, res.Token, 1, 100)
				_ = r.Complete(key, res.Token, []byte("v"), 2, 100)
			}
			_ = r.Snapshot()
			_, _ = r.Sweep(1000, 0)
		}(i)
	}
	wg.Wait()
	s := r.Snapshot()
	if s.Entries > 8 || s.ResultBytes > 64 {
		t.Fatalf("capacity violated: %+v", s)
	}
}
