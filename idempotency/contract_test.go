package idempotency

import (
	"bytes"
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
)

func newRegistry(t *testing.T, opts Options) *Registry {
	t.Helper()
	r, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return r
}

func TestOptionsAndValidationOrder(t *testing.T) {
	if _, err := New(Options{}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("New err=%v", err)
	}
	r := newRegistry(t, Options{MaxEntries: 1, MaxResultBytes: 2, MaxKeyBytes: 3})
	if _, err := r.Begin("", "f", -1, 0); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("validation order: %v", err)
	}
	if _, err := r.Begin("toolong", "f", 0, 1); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("long key: %v", err)
	}
	if _, err := r.Begin("k", "", 0, 1); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("fingerprint: %v", err)
	}
	if _, err := r.Begin("k", "f", -1, 1); !errors.Is(err, ErrInvalidTime) {
		t.Fatalf("time: %v", err)
	}
	if _, err := r.Begin("k", "f", math.MaxInt64, 1); !errors.Is(err, ErrInvalidTime) {
		t.Fatalf("overflow: %v", err)
	}
}

func TestBeginPendingConflictTakeoverAndStaleToken(t *testing.T) {
	r := newRegistry(t, Options{MaxEntries: 2, MaxResultBytes: 20, MaxKeyBytes: 20})
	a, err := r.Begin("k", "f1", 10, 5)
	if err != nil || !a.Leader || a.Token == 0 || a.LeaseUntil != 15 {
		t.Fatalf("leader=%+v err=%v", a, err)
	}
	p, err := r.Begin("k", "f1", 14, 9)
	if err != nil || !p.Pending || p.Token != 0 || p.LeaseUntil != 15 {
		t.Fatalf("pending=%+v err=%v", p, err)
	}
	if _, err := r.Begin("k", "f2", 14, 9); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflict=%v", err)
	}
	b, err := r.Begin("k", "f2", 15, 4)
	if err != nil || !b.Leader || b.Token <= a.Token {
		t.Fatalf("takeover=%+v err=%v", b, err)
	}
	if err := r.Complete("k", a.Token, []byte("x"), 15, 2); !errors.Is(err, ErrStaleToken) {
		t.Fatalf("old token=%v", err)
	}
	if got := r.Snapshot().Generation; got != 2 {
		t.Fatalf("generation=%d", got)
	}
}

func TestRenewCompleteReplayAndOwnership(t *testing.T) {
	r := newRegistry(t, Options{MaxEntries: 2, MaxResultBytes: 20, MaxKeyBytes: 20})
	b, _ := r.Begin("k", "f", 1, 4)
	if err := r.Renew("k", b.Token, 4, 6); err != nil {
		t.Fatal(err)
	}
	in := []byte("done")
	if err := r.Complete("k", b.Token, in, 9, 5); err != nil {
		t.Fatal(err)
	}
	in[0] = 'X'
	replay, err := r.Begin("k", "f", 13, 1)
	if err != nil || !replay.Replay || string(replay.Result) != "done" {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	replay.Result[0] = 'Y'
	if got, _ := r.Begin("k", "f", 13, 1); string(got.Result) != "done" {
		t.Fatalf("aliased=%q", got.Result)
	}
	if _, err := r.Begin("k", "other", 13, 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflict=%v", err)
	}
	next, err := r.Begin("k", "other", 14, 2)
	if err != nil || !next.Leader || next.Token <= b.Token {
		t.Fatalf("replacement=%+v err=%v", next, err)
	}
}

func TestCapacityRollbackAndTokenNotConsumed(t *testing.T) {
	r := newRegistry(t, Options{MaxEntries: 1, MaxResultBytes: 3, MaxKeyBytes: 20})
	a, _ := r.Begin("a", "f", 0, 10)
	before := r.Snapshot()
	if _, err := r.Begin("b", "f", 0, 10); !errors.Is(err, ErrCapacity) {
		t.Fatalf("capacity=%v", err)
	}
	if err := r.Complete("a", a.Token, []byte("four"), 1, 2); !errors.Is(err, ErrResultTooLarge) {
		t.Fatalf("large=%v", err)
	}
	after := r.Snapshot()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("failed operation mutated state\nbefore=%+v\nafter=%+v", before, after)
	}
	if err := r.Complete("a", a.Token, []byte("abc"), 1, 2); err != nil {
		t.Fatal(err)
	}
}

func TestAbortSweepOrderingLimitAndBoundary(t *testing.T) {
	r := newRegistry(t, Options{MaxEntries: 5, MaxResultBytes: 20, MaxKeyBytes: 20})
	a, _ := r.Begin("z", "f", 0, 5)
	if err := r.Abort("z", a.Token+1); !errors.Is(err, ErrStaleToken) {
		t.Fatalf("stale=%v", err)
	}
	if err := r.Abort("z", a.Token); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"c", "a", "b"} {
		if _, err := r.Begin(k, "f", 0, 5); err != nil {
			t.Fatal(err)
		}
	}
	keys, err := r.Sweep(5, 2)
	if err != nil || !reflect.DeepEqual(keys, []string{"a", "b"}) {
		t.Fatalf("keys=%v err=%v", keys, err)
	}
	keys, _ = r.Sweep(5, 0)
	if !reflect.DeepEqual(keys, []string{"c"}) {
		t.Fatalf("keys=%v", keys)
	}
}

func TestSnapshotSortShapeAndIsolation(t *testing.T) {
	r := newRegistry(t, Options{MaxEntries: 3, MaxResultBytes: 10, MaxKeyBytes: 20})
	b, _ := r.Begin("b", "fb", 1, 8)
	a, _ := r.Begin("a", "fa", 1, 8)
	if err := r.Complete("a", a.Token, []byte("ok"), 2, 8); err != nil {
		t.Fatal(err)
	}
	s := r.Snapshot()
	if s.Entries != 2 || s.ResultBytes != 2 || len(s.Records) != 2 || s.Records[0].Key != "a" || s.Records[1].Key != "b" {
		t.Fatalf("snapshot=%+v", s)
	}
	if s.Records[0].Pending || s.Records[0].LeaseUntil != 0 || s.Records[0].ReplayUntil != 10 {
		t.Fatalf("complete shape=%+v", s.Records[0])
	}
	if !s.Records[1].Pending || s.Records[1].ReplayUntil != 0 || len(s.Records[1].Result) != 0 || s.Records[1].Token != b.Token {
		t.Fatalf("pending shape=%+v", s.Records[1])
	}
	s.Records[0].Result[0] = 'X'
	if got := r.Snapshot().Records[0].Result; !bytes.Equal(got, []byte("ok")) {
		t.Fatalf("aliased=%q", got)
	}
}

func TestConcurrentBeginSingleLeader(t *testing.T) {
	r := newRegistry(t, Options{MaxEntries: 2, MaxResultBytes: 10, MaxKeyBytes: 20})
	const n = 32
	var wg sync.WaitGroup
	results := make(chan BeginResult, n)
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); v, err := r.Begin("k", "f", 10, 5); results <- v; errs <- err }()
	}
	wg.Wait()
	close(results)
	close(errs)
	leaders := 0
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for v := range results {
		if v.Leader {
			leaders++
		}
	}
	if leaders != 1 {
		t.Fatalf("leaders=%d", leaders)
	}
}
