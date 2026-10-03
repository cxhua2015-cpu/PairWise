package leasegraph

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestAddBatchFieldValidationOrder(t *testing.T) {
	s := newTestScheduler(t, 16, 1<<20, 1)
	cases := []struct {
		name string
		spec TaskSpec
		want error
	}{
		{"empty id", TaskSpec{ID: ""}, ErrInvalidID},
		{"bad char", TaskSpec{ID: "a/b"}, ErrInvalidID},
		{"too long id", TaskSpec{ID: string(make([]byte, 65))}, ErrInvalidID},
		{"priority low", TaskSpec{ID: "a", Priority: -1001}, ErrInvalidPriority},
		{"priority high", TaskSpec{ID: "a", Priority: 1001}, ErrInvalidPriority},
		{"payload too large", TaskSpec{ID: "a", Payload: make([]byte, 1<<20+1)}, ErrPayloadTooLarge},
		{"bad dep id", TaskSpec{ID: "a", Dependencies: []string{"!"}}, ErrInvalidID},
		{"dup dep", TaskSpec{ID: "a", Dependencies: []string{"b", "b"}}, ErrDuplicate},
	}
	for _, tc := range cases {
		if err := s.AddBatch([]TaskSpec{tc.spec}); !errors.Is(err, tc.want) {
			t.Fatalf("%s: got %v want %v", tc.name, err, tc.want)
		}
	}
	// ID validation precedes priority validation within the same task.
	if err := s.AddBatch([]TaskSpec{{ID: "!", Priority: 9999}}); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("precedence: %v", err)
	}
	// Boundary values are accepted.
	if err := s.AddBatch([]TaskSpec{{ID: "ok", Priority: 1000, Payload: make([]byte, 1<<20)}}); err != nil {
		t.Fatalf("boundary: %v", err)
	}
}

func TestAddBatchSelfCycleAndDepOnTerminal(t *testing.T) {
	s := newTestScheduler(t, 8, 1024, 1)
	if err := s.AddBatch([]TaskSpec{{ID: "self", Dependencies: []string{"self"}}}); !errors.Is(err, ErrCycle) {
		t.Fatalf("self cycle: %v", err)
	}
	if err := s.AddBatch([]TaskSpec{{ID: "a"}, {ID: "b", Dependencies: []string{"a"}}}); err != nil {
		t.Fatal(err)
	}
	l, _ := s.Claim(0)
	if _, err := s.Complete(1, l.ID, l.Token, nil, false); err != nil {
		t.Fatal(err)
	}
	// Dependency on a failed task keeps the new task blocked forever.
	if err := s.AddBatch([]TaskSpec{{ID: "c", Dependencies: []string{"a"}}}); err != nil {
		t.Fatal(err)
	}
	snap := s.Snapshot()
	if taskByID(t, snap, "c").State != StateBlocked || taskByID(t, snap, "b").State != StateCanceled {
		t.Fatalf("snap=%+v", snap)
	}
	if _, err := s.Claim(0); !errors.Is(err, ErrNoReady) {
		t.Fatalf("claim: %v", err)
	}
}

func TestClaimReadyOrderingAndDeadlineBoundary(t *testing.T) {
	s := newTestScheduler(t, 8, 1024, 1)
	if err := s.AddBatch([]TaskSpec{
		{ID: "b", Priority: 5},
		{ID: "a", Priority: 5},
		{ID: "z", Priority: -3},
	}); err != nil {
		t.Fatal(err)
	}
	l1, _ := s.Claim(0)
	l2, _ := s.Claim(0)
	l3, _ := s.Claim(0)
	if l1.ID != "a" || l2.ID != "b" || l3.ID != "z" {
		t.Fatalf("order: %s %s %s", l1.ID, l2.ID, l3.ID)
	}
	if l1.Token == 0 || l2.Token <= l1.Token || l3.Token <= l2.Token {
		t.Fatalf("tokens: %d %d %d", l1.Token, l2.Token, l3.Token)
	}
	if _, err := s.Claim(0); !errors.Is(err, ErrNoReady) {
		t.Fatalf("no ready: %v", err)
	}
	// now == deadline-1 succeeds, now == deadline is stale.
	if _, err := s.Complete(9, l1.ID, l1.Token, nil, true); err != nil {
		t.Fatalf("boundary complete: %v", err)
	}
	if _, err := s.Complete(10, l2.ID, l2.Token, nil, true); !errors.Is(err, ErrStaleLease) {
		t.Fatalf("stale: %v", err)
	}
	// now+LeaseDuration overflowing MaxTime rejects without consuming a token.
	s2 := newTestScheduler(t, 4, 64, 1)
	if err := s2.AddBatch([]TaskSpec{{ID: "x"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s2.Claim(MaxTime - 5); !errors.Is(err, ErrInvalidTime) {
		t.Fatalf("overflow: %v", err)
	}
	l, err := s2.Claim(MaxTime - 10)
	if err != nil || l.Token != 1 || l.Deadline != MaxTime {
		t.Fatalf("lease=%+v err=%v", l, err)
	}
	if _, err := s2.Claim(MaxTime + 1); !errors.Is(err, ErrInvalidTime) {
		t.Fatalf("time: %v", err)
	}
	if _, err := s2.Claim(-1); !errors.Is(err, ErrInvalidTime) {
		t.Fatalf("neg time: %v", err)
	}
}

func TestCompleteValidationOrderAndTokenMismatch(t *testing.T) {
	s := newTestScheduler(t, 4, 64, 2)
	if err := s.AddBatch([]TaskSpec{{ID: "a"}}); err != nil {
		t.Fatal(err)
	}
	l, _ := s.Claim(0)
	if _, err := s.Complete(1, "a", l.Token+1, nil, true); !errors.Is(err, ErrStaleLease) {
		t.Fatalf("token: %v", err)
	}
	if _, err := s.Complete(1, "a", l.Token, make([]byte, 1<<20+1), true); !errors.Is(err, ErrInvalidResult) {
		t.Fatalf("result size: %v", err)
	}
	if _, err := s.Complete(1, "a", l.Token, nil, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Complete(1, "a", l.Token, nil, true); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("not running: %v", err)
	}
}

func TestSweepOrderingAndCascadeSkip(t *testing.T) {
	s := newTestScheduler(t, 8, 1024, 1)
	if err := s.AddBatch([]TaskSpec{
		{ID: "a"},
		{ID: "b"},
		{ID: "c", Dependencies: []string{"a"}},
		{ID: "d", Dependencies: []string{"c"}},
	}); err != nil {
		t.Fatal(err)
	}
	la, _ := s.Claim(0) // a, deadline 10
	lb, _ := s.Claim(5) // b, deadline 15
	sr, err := s.Sweep(20)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(sr.Expired) != "[a b]" || fmt.Sprint(sr.Failed) != "[a b]" ||
		fmt.Sprint(sr.Canceled) != "[c d]" {
		t.Fatalf("sweep=%+v", sr)
	}
	_ = la
	_ = lb
	// Second sweep with nothing expired returns an empty result.
	sr2, err := s.Sweep(100)
	if err != nil || sr2.Expired != nil || sr2.Ready != nil {
		t.Fatalf("sweep2=%+v err=%v", sr2, err)
	}
	if _, err := s.Sweep(MaxTime + 1); !errors.Is(err, ErrInvalidTime) {
		t.Fatalf("sweep time: %v", err)
	}
}

func TestSweepRetryDoesNotIncrementAttempt(t *testing.T) {
	s := newTestScheduler(t, 4, 64, 2)
	if err := s.AddBatch([]TaskSpec{{ID: "a"}}); err != nil {
		t.Fatal(err)
	}
	l1, _ := s.Claim(0)
	sr, _ := s.Sweep(10)
	if fmt.Sprint(sr.Ready) != "[a]" {
		t.Fatalf("sweep=%+v", sr)
	}
	l2, _ := s.Claim(10)
	if l2.Attempt != 2 || l2.Token <= l1.Token {
		t.Fatalf("l2=%+v", l2)
	}
	// Attempt is now 2 == MaxAttempts; next expiry fails terminally.
	sr2, _ := s.Sweep(20)
	if fmt.Sprint(sr2.Failed) != "[a]" {
		t.Fatalf("sweep2=%+v", sr2)
	}
}

func TestSnapshotReadyOrderAndLeaseFields(t *testing.T) {
	s := newTestScheduler(t, 8, 1024, 2)
	if err := s.AddBatch([]TaskSpec{
		{ID: "x", Priority: 1},
		{ID: "y", Priority: 7},
		{ID: "z", Priority: 7},
	}); err != nil {
		t.Fatal(err)
	}
	snap := s.Snapshot()
	if fmt.Sprint(snap.Ready) != "[y z x]" {
		t.Fatalf("ready=%v", snap.Ready)
	}
	l, _ := s.Claim(0)
	snap = s.Snapshot()
	v := taskByID(t, snap, l.ID)
	if v.LeaseToken != l.Token || v.Deadline != 10 || v.State != StateRunning {
		t.Fatalf("view=%+v", v)
	}
	for _, other := range snap.Tasks {
		if other.ID != l.ID && (other.LeaseToken != 0 || other.Deadline != 0) {
			t.Fatalf("non-running lease fields: %+v", other)
		}
	}
}

func TestConcurrentMixedOperations(t *testing.T) {
	s := newTestScheduler(t, 256, 1<<20, 3)
	for i := 0; i < 8; i++ {
		specs := make([]TaskSpec, 16)
		for j := range specs {
			specs[j] = TaskSpec{ID: fmt.Sprintf("t-%d-%d", i, j), Priority: j % 4}
		}
		if err := s.AddBatch(specs); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(now int64) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				l, err := s.Claim(now)
				if err != nil {
					_ = s.Snapshot()
					_, _ = s.Sweep(now + 100)
					continue
				}
				if i%3 == 0 {
					_, _ = s.Complete(now+1, l.ID, l.Token, nil, false)
				} else {
					_, _ = s.Complete(now+1, l.ID, l.Token, []byte("r"), true)
				}
			}
		}(int64(w * 10))
	}
	wg.Wait()
	snap := s.Snapshot()
	tokens := map[uint64]bool{}
	for _, task := range snap.Tasks {
		if task.State == StateRunning {
			if tokens[task.LeaseToken] {
				t.Fatalf("duplicate live token %d", task.LeaseToken)
			}
			tokens[task.LeaseToken] = true
		}
	}
}
