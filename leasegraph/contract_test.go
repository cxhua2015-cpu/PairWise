package leasegraph

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func newTestScheduler(t *testing.T, maxTasks, maxBytes int, attempts uint32) *Scheduler {
	t.Helper()
	s, err := New(Options{MaxTasks: maxTasks, MaxBytes: maxBytes, LeaseDuration: 10, MaxAttempts: attempts})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func taskByID(t *testing.T, s Snapshot, id string) TaskView {
	t.Helper()
	for _, task := range s.Tasks {
		if task.ID == id {
			return task
		}
	}
	t.Fatalf("task %q missing", id)
	return TaskView{}
}

func TestNewValidation(t *testing.T) {
	bad := []Options{
		{},
		{MaxTasks: 1, MaxBytes: 1, LeaseDuration: 0, MaxAttempts: 1},
		{MaxTasks: 10001, MaxBytes: 1, LeaseDuration: 1, MaxAttempts: 1},
		{MaxTasks: 1, MaxBytes: 64<<20 + 1, LeaseDuration: 1, MaxAttempts: 1},
		{MaxTasks: 1, MaxBytes: 1, LeaseDuration: 1, MaxAttempts: 101},
	}
	for _, opts := range bad {
		if _, err := New(opts); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("opts=%+v err=%v", opts, err)
		}
	}
}

func TestAddBatchForwardReferenceOrderingAndOwnership(t *testing.T) {
	s := newTestScheduler(t, 8, 1024, 2)
	payload := []byte("root")
	deps := []string{"root"}
	err := s.AddBatch([]TaskSpec{
		{ID: "child-low", Dependencies: deps, Priority: 1},
		{ID: "child-high", Dependencies: []string{"root"}, Priority: 9},
		{ID: "root", Priority: -1, Payload: payload},
	})
	if err != nil {
		t.Fatal(err)
	}
	payload[0] = 'X'
	deps[0] = "evil"
	snap := s.Snapshot()
	if fmt.Sprint(snap.Ready) != "[root]" || string(taskByID(t, snap, "root").Payload) != "root" {
		t.Fatalf("snapshot=%+v", snap)
	}
	if got := taskByID(t, snap, "child-low").Dependencies; fmt.Sprint(got) != "[root]" {
		t.Fatalf("deps=%v", got)
	}
	l, err := s.Claim(0)
	if err != nil || l.ID != "root" || l.Deadline != 10 || l.Attempt != 1 {
		t.Fatalf("lease=%+v err=%v", l, err)
	}
	l.Payload[0] = 'Y'
	tr, err := s.Complete(1, l.ID, l.Token, []byte("ok"), true)
	if err != nil || fmt.Sprint(tr.Ready) != "[child-high child-low]" {
		t.Fatalf("tr=%+v err=%v", tr, err)
	}
	next, _ := s.Claim(1)
	if next.ID != "child-high" {
		t.Fatalf("next=%+v", next)
	}
}

func TestAddBatchRollbackUnknownCycleDuplicateAndCapacity(t *testing.T) {
	s := newTestScheduler(t, 2, 4, 1)
	if err := s.AddBatch([]TaskSpec{{ID: "a", Dependencies: []string{"missing"}}}); !errors.Is(err, ErrUnknownDependency) {
		t.Fatalf("unknown=%v", err)
	}
	if err := s.AddBatch([]TaskSpec{{ID: "a", Dependencies: []string{"b"}}, {ID: "b", Dependencies: []string{"a"}}}); !errors.Is(err, ErrCycle) {
		t.Fatalf("cycle=%v", err)
	}
	if len(s.Snapshot().Tasks) != 0 {
		t.Fatal("failed batch mutated state")
	}
	if err := s.AddBatch([]TaskSpec{{ID: "a", Payload: []byte("123")}}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddBatch([]TaskSpec{{ID: "a"}}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate=%v", err)
	}
	if err := s.AddBatch([]TaskSpec{{ID: "b", Payload: []byte("12")}}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("capacity=%v", err)
	}
	if len(s.Snapshot().Tasks) != 1 || s.Snapshot().UsedBytes != 3 {
		t.Fatalf("snapshot=%+v", s.Snapshot())
	}
}

func TestLeaseBoundaryRetryTokenAndCascade(t *testing.T) {
	s := newTestScheduler(t, 5, 1024, 2)
	if err := s.AddBatch([]TaskSpec{{ID: "root"}, {ID: "mid", Dependencies: []string{"root"}}, {ID: "leaf", Dependencies: []string{"mid"}}}); err != nil {
		t.Fatal(err)
	}
	l1, _ := s.Claim(10)
	if _, err := s.Complete(20, "root", l1.Token, nil, true); !errors.Is(err, ErrStaleLease) {
		t.Fatalf("deadline err=%v", err)
	}
	sr, err := s.Sweep(20)
	if err != nil || fmt.Sprint(sr.Expired) != "[root]" || fmt.Sprint(sr.Ready) != "[root]" {
		t.Fatalf("sweep=%+v err=%v", sr, err)
	}
	l2, _ := s.Claim(20)
	if l2.Token == l1.Token || l2.Attempt != 2 {
		t.Fatalf("leases=%+v %+v", l1, l2)
	}
	tr, err := s.Complete(21, "root", l2.Token, nil, false)
	if err != nil || fmt.Sprint(tr.Failed) != "[root]" || fmt.Sprint(tr.Canceled) != "[leaf mid]" {
		t.Fatalf("tr=%+v err=%v", tr, err)
	}
	snap := s.Snapshot()
	if taskByID(t, snap, "root").State != StateFailed || taskByID(t, snap, "mid").State != StateCanceled {
		t.Fatalf("snap=%+v", snap)
	}
}

func TestResultCapacityRollbackAndOwnership(t *testing.T) {
	s := newTestScheduler(t, 2, 5, 1)
	input := []byte("abc")
	if err := s.AddBatch([]TaskSpec{{ID: "a", Payload: input}}); err != nil {
		t.Fatal(err)
	}
	l, _ := s.Claim(0)
	if _, err := s.Complete(1, "a", l.Token, []byte("xyz"), true); !errors.Is(err, ErrCapacity) {
		t.Fatalf("capacity=%v", err)
	}
	if taskByID(t, s.Snapshot(), "a").State != StateRunning {
		t.Fatal("capacity failure mutated state")
	}
	result := []byte("ok")
	if _, err := s.Complete(1, "a", l.Token, result, true); err != nil {
		t.Fatal(err)
	}
	result[0] = 'X'
	s1 := s.Snapshot()
	if string(taskByID(t, s1, "a").Result) != "ok" || s1.UsedBytes != 5 {
		t.Fatalf("snapshot=%+v", s1)
	}
	v := taskByID(t, s1, "a")
	v.Payload[0] = 'Y'
	v.Result[0] = 'Y'
	s2 := s.Snapshot()
	if string(taskByID(t, s2, "a").Payload) != "abc" || string(taskByID(t, s2, "a").Result) != "ok" {
		t.Fatal("snapshot aliases state")
	}
}

func TestValidationPrecedenceAndNoTokenConsumption(t *testing.T) {
	s := newTestScheduler(t, 2, 32, 1)
	if err := s.AddBatch([]TaskSpec{{ID: "a"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim(MaxTime); !errors.Is(err, ErrInvalidTime) {
		t.Fatalf("time=%v", err)
	}
	l, err := s.Claim(0)
	if err != nil || l.Token != 1 {
		t.Fatalf("lease=%+v err=%v", l, err)
	}
	if _, err := s.Complete(-1, "!", 0, nil, true); !errors.Is(err, ErrInvalidTime) {
		t.Fatalf("precedence=%v", err)
	}
	if _, err := s.Complete(1, "!", 0, nil, true); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("id=%v", err)
	}
	if _, err := s.Complete(1, "missing", 0, nil, true); !errors.Is(err, ErrUnknownTask) {
		t.Fatalf("missing=%v", err)
	}
	if _, err := s.Complete(1, "a", l.Token, []byte("bad"), false); !errors.Is(err, ErrInvalidResult) {
		t.Fatalf("result=%v", err)
	}
}

func TestConcurrentClaimsAndSnapshots(t *testing.T) {
	s := newTestScheduler(t, 64, 4096, 1)
	specs := make([]TaskSpec, 32)
	for i := range specs {
		specs[i] = TaskSpec{ID: fmt.Sprintf("t-%02d", i), Priority: i % 3, Payload: []byte{byte(i)}}
	}
	if err := s.AddBatch(specs); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	ids := make(chan string, len(specs))
	for range specs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l, err := s.Claim(0)
			if err != nil {
				t.Errorf("claim: %v", err)
				return
			}
			ids <- l.ID
			if _, err := s.Complete(1, l.ID, l.Token, nil, true); err != nil {
				t.Errorf("complete: %v", err)
			}
			_ = s.Snapshot()
		}()
	}
	wg.Wait()
	close(ids)
	seen := map[string]bool{}
	for id := range ids {
		if seen[id] {
			t.Fatalf("duplicate claim %s", id)
		}
		seen[id] = true
	}
	if len(seen) != len(specs) {
		t.Fatalf("claimed=%d", len(seen))
	}
}
