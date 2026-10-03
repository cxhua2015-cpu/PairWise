package leasegraph

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
)

func TestOptionsBoundary(t *testing.T) {
	good := []Options{
		{MaxTasks: 1, MaxBytes: 1, LeaseDuration: 1, MaxAttempts: 1},
		{MaxTasks: 10000, MaxBytes: 64 << 20, LeaseDuration: 1_000_000_000_000, MaxAttempts: 100},
	}
	for _, opts := range good {
		if _, err := New(opts); err != nil {
			t.Fatalf("opts=%+v err=%v", opts, err)
		}
	}
	bad := []Options{
		{MaxTasks: 0, MaxBytes: 1, LeaseDuration: 1, MaxAttempts: 1},
		{MaxTasks: 1, MaxBytes: 0, LeaseDuration: 1, MaxAttempts: 1},
		{MaxTasks: 1, MaxBytes: 1, LeaseDuration: 1, MaxAttempts: 0},
		{MaxTasks: 1, MaxBytes: 1, LeaseDuration: 1_000_000_000_001, MaxAttempts: 1},
	}
	for _, opts := range bad {
		if _, err := New(opts); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("opts=%+v err=%v", opts, err)
		}
	}
}

func TestAddBatchFieldValidationOrder(t *testing.T) {
	s := newTestScheduler(t, 16, 1<<20, 1)
	cases := []struct {
		spec TaskSpec
		want error
	}{
		{TaskSpec{ID: ""}, ErrInvalidID},
		{TaskSpec{ID: string(make([]byte, 65))}, ErrInvalidID},
		{TaskSpec{ID: "bad id"}, ErrInvalidID},
		{TaskSpec{ID: "ok", Priority: 1001}, ErrInvalidPriority},
		{TaskSpec{ID: "ok", Priority: -1001}, ErrInvalidPriority},
		{TaskSpec{ID: "ok", Payload: make([]byte, 1<<20+1)}, ErrPayloadTooLarge},
		{TaskSpec{ID: "ok", Dependencies: []string{"bad dep!"}}, ErrInvalidID},
		{TaskSpec{ID: "ok", Dependencies: []string{"x", "x"}}, ErrDuplicate},
	}
	for _, tc := range cases {
		if err := s.AddBatch([]TaskSpec{tc.spec}); !errors.Is(err, tc.want) {
			t.Fatalf("spec=%+v err=%v want=%v", tc.spec, err, tc.want)
		}
	}
	// 空批次成功且无副作用。
	if err := s.AddBatch(nil); err != nil {
		t.Fatal(err)
	}
	if len(s.Snapshot().Tasks) != 0 {
		t.Fatal("empty batch mutated state")
	}
	// 批内重复 ID。
	err := s.AddBatch([]TaskSpec{{ID: "a"}, {ID: "a"}})
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("dup=%v", err)
	}
	// 单任务自依赖成环。
	if err := s.AddBatch([]TaskSpec{{ID: "s", Dependencies: []string{"s"}}}); !errors.Is(err, ErrCycle) {
		t.Fatalf("self cycle=%v", err)
	}
	// MaxTasks 容量。
	if err := s.AddBatch([]TaskSpec{{ID: "a"}, {ID: "b"}}); err != nil {
		t.Fatal(err)
	}
	s2 := newTestScheduler(t, 2, 1<<20, 1)
	if err := s2.AddBatch([]TaskSpec{{ID: "x"}, {ID: "y"}, {ID: "z"}}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("task capacity=%v", err)
	}
	if len(s2.Snapshot().Tasks) != 0 {
		t.Fatal("capacity failure mutated state")
	}
}

func TestClaimBoundariesAndTokenSequence(t *testing.T) {
	s := newTestScheduler(t, 4, 1024, 3)
	if _, err := s.Claim(-1); !errors.Is(err, ErrInvalidTime) {
		t.Fatalf("neg time=%v", err)
	}
	if _, err := s.Claim(MaxTime + 1); !errors.Is(err, ErrInvalidTime) {
		t.Fatalf("over time=%v", err)
	}
	if _, err := s.Claim(0); !errors.Is(err, ErrNoReady) {
		t.Fatalf("no ready=%v", err)
	}
	if err := s.AddBatch([]TaskSpec{{ID: "a", Priority: 1}, {ID: "b", Priority: 1}, {ID: "c", Priority: 2}}); err != nil {
		t.Fatal(err)
	}
	// 优先级降序，再按 ID 升序。
	l1, _ := s.Claim(0)
	if l1.ID != "c" || l1.Token != 1 || l1.Deadline != 10 {
		t.Fatalf("l1=%+v", l1)
	}
	l2, _ := s.Claim(0)
	if l2.ID != "a" || l2.Token != 2 {
		t.Fatalf("l2=%+v", l2)
	}
	// 截止时间恰为 MaxTime 合法。
	s3 := newTestScheduler(t, 1, 8, 1)
	if err := s3.AddBatch([]TaskSpec{{ID: "z"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s3.Claim(MaxTime - 10); err != nil {
		t.Fatalf("boundary claim=%v", err)
	}
}

func TestCompleteValidationAndRetry(t *testing.T) {
	s := newTestScheduler(t, 4, 1024, 2)
	if err := s.AddBatch([]TaskSpec{{ID: "a"}, {ID: "b"}}); err != nil {
		t.Fatal(err)
	}
	l, _ := s.Claim(0)
	// token 不匹配。
	if _, err := s.Complete(1, l.ID, l.Token+99, nil, true); !errors.Is(err, ErrStaleLease) {
		t.Fatalf("token=%v", err)
	}
	// 非 running 任务。
	if _, err := s.Complete(1, "b", 1, nil, true); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("not running=%v", err)
	}
	// Result 过大。
	if _, err := s.Complete(1, l.ID, l.Token, make([]byte, 1<<20+1), true); !errors.Is(err, ErrInvalidResult) {
		t.Fatalf("big result=%v", err)
	}
	// 失败后可重试，Attempt 递增。
	tr, err := s.Complete(1, l.ID, l.Token, nil, false)
	if err != nil || len(tr.Failed) != 0 {
		t.Fatalf("retry tr=%+v err=%v", tr, err)
	}
	l2, _ := s.Claim(2)
	if l2.ID != "a" || l2.Attempt != 2 {
		t.Fatalf("l2=%+v", l2)
	}
	if _, err := s.Complete(3, l2.ID, l2.Token, nil, true); err != nil {
		t.Fatal(err)
	}
	if got := taskByID(t, s.Snapshot(), "a"); got.State != StateSucceeded || got.LeaseToken != 0 || got.Deadline != 0 {
		t.Fatalf("view=%+v", got)
	}
}

func TestSweepOrderingAndCascade(t *testing.T) {
	s := newTestScheduler(t, 8, 1024, 1)
	specs := []TaskSpec{
		{ID: "a"}, {ID: "b"}, {ID: "c"},
		{ID: "d", Dependencies: []string{"a"}},
		{ID: "e", Dependencies: []string{"d"}},
	}
	if err := s.AddBatch(specs); err != nil {
		t.Fatal(err)
	}
	la, _ := s.Claim(0)  // a, deadline 10
	lb, _ := s.Claim(5)  // b, deadline 15
	lc, _ := s.Claim(10) // c, deadline 20
	_ = lb
	_ = lc
	// 恰好到期即处理；顺序 deadline 升序再 ID 升序：a(10), b(15), c(20)。
	sr, err := s.Sweep(20)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(sr.Expired) != "[a b c]" {
		t.Fatalf("expired=%v", sr.Expired)
	}
	// MaxAttempts=1：全部终止失败；a 的失败级联取消 d、e。
	if fmt.Sprint(sr.Failed) != "[a b c]" || fmt.Sprint(sr.Canceled) != "[d e]" {
		t.Fatalf("sweep=%+v", sr)
	}
	if len(sr.Ready) != 0 {
		t.Fatalf("ready=%v", sr.Ready)
	}
	// 无到期任务返回空结果。
	sr2, err := s.Sweep(100)
	if err != nil || len(sr2.Expired) != 0 {
		t.Fatalf("sweep2=%+v err=%v", sr2, err)
	}
	if _, err := s.Sweep(MaxTime + 1); !errors.Is(err, ErrInvalidTime) {
		t.Fatalf("sweep time=%v", err)
	}
	_ = la
}

func TestSweepRetryRequeues(t *testing.T) {
	s := newTestScheduler(t, 4, 1024, 2)
	if err := s.AddBatch([]TaskSpec{{ID: "a"}, {ID: "b"}}); err != nil {
		t.Fatal(err)
	}
	l1, _ := s.Claim(0)
	l2, _ := s.Claim(0)
	sr, err := s.Sweep(10)
	if err != nil || fmt.Sprint(sr.Expired) != "[a b]" || fmt.Sprint(sr.Ready) != "[a b]" {
		t.Fatalf("sweep=%+v err=%v", sr, err)
	}
	l3, _ := s.Claim(20)
	if l3.Attempt != 2 || l3.Token == l1.Token || l3.Token == l2.Token {
		t.Fatalf("l3=%+v", l3)
	}
}

func TestSnapshotOrderingAndIsolation(t *testing.T) {
	s := newTestScheduler(t, 8, 1024, 2)
	if err := s.AddBatch([]TaskSpec{
		{ID: "b", Priority: 5, Payload: []byte("B")},
		{ID: "a", Priority: 5, Payload: []byte("A")},
		{ID: "c", Dependencies: []string{"a", "b"}, Priority: 9},
	}); err != nil {
		t.Fatal(err)
	}
	snap := s.Snapshot()
	ids := []string{snap.Tasks[0].ID, snap.Tasks[1].ID, snap.Tasks[2].ID}
	if fmt.Sprint(ids) != "[a b c]" {
		t.Fatalf("order=%v", ids)
	}
	if fmt.Sprint(snap.Ready) != "[a b]" {
		t.Fatalf("ready=%v", snap.Ready)
	}
	if got := taskByID(t, snap, "c"); got.State != StateBlocked || fmt.Sprint(got.Dependencies) != "[a b]" {
		t.Fatalf("c=%+v", got)
	}
	// 修改快照切片不影响内部。
	snap.Ready[0] = "evil"
	snap.Tasks[2].Dependencies[0] = "evil"
	s2 := s.Snapshot()
	if s2.Ready[0] != "a" || len(taskByID(t, s2, "c").Dependencies) != 2 {
		t.Fatal("snapshot aliases state")
	}
	// Lease.Payload 与内部隔离。
	l, _ := s.Claim(0)
	l.Payload[0] = 'Z'
	if string(taskByID(t, s.Snapshot(), l.ID).Payload) != "A" {
		t.Fatal("lease payload aliases state")
	}
}

func TestConcurrentMixedOperations(t *testing.T) {
	s := newTestScheduler(t, 128, 1<<20, 2)
	var wg sync.WaitGroup
	// 并发 AddBatch（互不重叠的 ID 段）。
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 8; i++ {
				_ = s.AddBatch([]TaskSpec{{ID: fmt.Sprintf("g%d-%d", g, i), Payload: []byte{byte(i)}}})
			}
		}(g)
	}
	wg.Wait()
	if n := len(s.Snapshot().Tasks); n != 32 {
		t.Fatalf("tasks=%d", n)
	}
	tokens := make(chan uint64, 64)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				l, err := s.Claim(0)
				if errors.Is(err, ErrNoReady) {
					return
				}
				if err != nil {
					t.Errorf("claim: %v", err)
					return
				}
				tokens <- l.Token
				if _, err := s.Complete(1, l.ID, l.Token, []byte("r"), true); err != nil {
					t.Errorf("complete: %v", err)
				}
				_, _ = s.Sweep(2)
				_ = s.Snapshot()
			}
		}()
	}
	wg.Wait()
	close(tokens)
	seen := map[uint64]bool{}
	for tok := range tokens {
		if tok == 0 || seen[tok] {
			t.Fatalf("bad token %d", tok)
		}
		seen[tok] = true
	}
	snap := s.Snapshot()
	for _, tv := range snap.Tasks {
		if tv.State != StateSucceeded {
			t.Fatalf("task %s state=%s", tv.ID, tv.State)
		}
	}
	if snap.UsedBytes != 32+32 {
		t.Fatalf("used=%d", snap.UsedBytes)
	}
}

func TestReadyOrderAfterUnblock(t *testing.T) {
	s := newTestScheduler(t, 8, 1024, 1)
	if err := s.AddBatch([]TaskSpec{
		{ID: "root"},
		{ID: "z-low", Dependencies: []string{"root"}, Priority: 1},
		{ID: "a-high", Dependencies: []string{"root"}, Priority: 9},
		{ID: "m-mid", Dependencies: []string{"root"}, Priority: 5},
	}); err != nil {
		t.Fatal(err)
	}
	l, _ := s.Claim(0)
	tr, err := s.Complete(1, l.ID, l.Token, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(tr.Ready) != "[a-high m-mid z-low]" {
		t.Fatalf("ready=%v", tr.Ready)
	}
	got := []string{}
	for {
		l, err := s.Claim(2)
		if errors.Is(err, ErrNoReady) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, l.ID)
		if _, err := s.Complete(3, l.ID, l.Token, nil, true); err != nil {
			t.Fatal(err)
		}
	}
	if !slices.Equal(got, []string{"a-high", "m-mid", "z-low"}) {
		t.Fatalf("claim order=%v", got)
	}
}
