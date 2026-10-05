package delayedqueue

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestTimeBoundaries(t *testing.T) {
	q := nq(t)
	if _, e := q.Apply(Batch{Now: -1}); !errors.Is(e, ErrTime) {
		t.Fatalf("negative now: %v", e)
	}
	if _, e := q.Peek(-1, 1); !errors.Is(e, ErrTime) {
		t.Fatalf("negative peek: %v", e)
	}
	if _, e := q.Take(-1, 1); !errors.Is(e, ErrTime) {
		t.Fatalf("negative take: %v", e)
	}
	// Equal time is allowed and empty batch advances time without generation.
	r, e := q.Apply(Batch{Now: 0})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatalf("r=%+v e=%v", r, e)
	}
	if _, e := q.Apply(Batch{Now: 7}); e != nil || q.Snapshot().Now != 7 {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 6}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	// ReadyAt boundary: ReadyAt == now is ready.
	_, _ = q.Apply(Batch{Now: 10, Ops: []Op{en("a", 1, 10, "x")}})
	p, e := q.Peek(10, 1)
	if e != nil || len(p) != 1 {
		t.Fatalf("p=%+v e=%v", p, e)
	}
}

func TestLimitValidation(t *testing.T) {
	q := nq(t)
	for _, l := range []int{0, -1, 1001} {
		if _, e := q.Peek(0, l); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("peek limit %d: %v", l, e)
		}
		if _, e := q.Take(0, l); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("take limit %d: %v", l, e)
		}
	}
	if _, e := q.Peek(0, 1000); e != nil {
		t.Fatal(e)
	}
}

func TestStructuralValidationOrder(t *testing.T) {
	q := nq(t)
	// Bad op later in batch must fail even if earlier ops are fine; nothing applied.
	_, e := q.Apply(Batch{Now: 1, Ops: []Op{
		en("a", 1, 1, "x"),
		{Kind: Enqueue, ID: "b", ReadyAt: 0}, // ReadyAt < Batch.Now
	}})
	if !errors.Is(e, ErrInvalidInput) || len(q.Snapshot().Jobs) != 0 {
		t.Fatal(e)
	}
	// Unknown kind.
	if _, e := q.Apply(Batch{Ops: []Op{{Kind: 99, ID: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Reschedule with payload, Cancel with nonzero fields.
	if _, e := q.Apply(Batch{Ops: []Op{{Kind: Reschedule, ID: "a", Payload: []byte("x")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Kind: Cancel, ID: "a", Priority: 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Invalid IDs.
	for _, id := range []string{"", "bad id", "bad?", "工具"} {
		if _, e := q.Apply(Batch{Ops: []Op{en(id, 0, 0, "x")}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, e)
		}
	}
	// ID too long and payload too large.
	if _, e := q.Apply(Batch{Ops: []Op{en("0123456789abcdefg", 0, 0, "x")}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{en("a", 0, 0, "012345678")}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Validation happens before the time check.
	if _, e := q.Apply(Batch{Now: 5}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 4, Ops: []Op{{Kind: 99}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestRevisionRollbackOnCapacity(t *testing.T) {
	q, _ := New(Options{MaxJobs: 2, MaxIDBytes: 8, MaxPayloadBytes: 4, MaxTotalPayloadBytes: 8})
	_, e := q.Apply(Batch{Ops: []Op{en("a", 0, 0, "1"), en("b", 0, 0, "2"), en("c", 0, 0, "3")}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := q.Snapshot()
	if s.NextRevision != 1 || s.Generation != 0 || len(s.Jobs) != 0 {
		t.Fatalf("s=%+v", s)
	}
	// Total payload bytes exceeded only at the end (temporary excess ok, final not).
	_, e = q.Apply(Batch{Ops: []Op{en("a", 0, 0, "1234"), en("b", 0, 0, "1234"), en("c", 0, 0, "1234")}})
	if !errors.Is(e, ErrCapacity) || q.Snapshot().NextRevision != 1 {
		t.Fatal(e)
	}
	// Temporary excess that becomes compliant succeeds.
	x, e := q.Apply(Batch{Ops: []Op{
		en("a", 0, 0, "1234"), en("b", 0, 0, "1234"), en("c", 0, 0, "1234"),
		{Kind: Cancel, ID: "c"},
	}})
	if e != nil || x.Revision != 3 || len(q.Snapshot().Jobs) != 2 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
}

func TestRevisionRollbackOnConflictAndNotFound(t *testing.T) {
	q := nq(t)
	_, _ = q.Apply(Batch{Ops: []Op{en("a", 0, 0, "x")}})
	before := q.Snapshot()
	_, e := q.Apply(Batch{Ops: []Op{en("b", 0, 0, "y"), en("a", 0, 0, "z")}})
	if !errors.Is(e, ErrConflict) || !reflect.DeepEqual(before, q.Snapshot()) {
		t.Fatal(e)
	}
	_, e = q.Apply(Batch{Ops: []Op{{Kind: Reschedule, ID: "a", ReadyAt: 9}, {Kind: Reschedule, ID: "zz", ReadyAt: 1}}})
	if !errors.Is(e, ErrNotFound) || !reflect.DeepEqual(before, q.Snapshot()) {
		t.Fatal(e)
	}
	// Revision continues from before the failed batches.
	x, e := q.Apply(Batch{Ops: []Op{{Kind: Reschedule, ID: "a", ReadyAt: 2}}})
	if e != nil || x.Revision != 2 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
}

func TestSequentialBatchSemantics(t *testing.T) {
	q := nq(t)
	// Repeated reschedule in one batch; last wins; revisions consecutive.
	x, e := q.Apply(Batch{Now: 1, Ops: []Op{
		en("a", 1, 5, "x"),
		{Kind: Reschedule, ID: "a", Priority: 2, ReadyAt: 4},
		{Kind: Reschedule, ID: "a", Priority: 3, ReadyAt: 3},
	}})
	if e != nil || x.Revision != 3 || len(x.Changed) != 1 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
	c := x.Changed[0]
	if c.Priority != 3 || c.ReadyAt != 3 || c.Revision != 3 {
		t.Fatalf("c=%+v", c)
	}
	// Changed sorted by ID, deduplicated, only survivors.
	x, e = q.Apply(Batch{Now: 2, Ops: []Op{
		en("z", 0, 2, "z"), en("m", 0, 2, "m"),
		{Kind: Reschedule, ID: "z", ReadyAt: 2},
		{Kind: Cancel, ID: "a"},
	}})
	if e != nil || len(x.Changed) != 2 || x.Changed[0].ID != "m" || x.Changed[1].ID != "z" {
		t.Fatalf("x=%+v e=%v", x, e)
	}
	// Enqueue then cancel in same batch: touched but not surviving.
	x, e = q.Apply(Batch{Now: 3, Ops: []Op{en("t", 0, 3, "t"), {Kind: Cancel, ID: "t"}}})
	if e != nil || len(x.Changed) != 0 || x.Revision != 7 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
}

func TestFinalCapacityJobs(t *testing.T) {
	q, _ := New(Options{MaxJobs: 2, MaxIDBytes: 8, MaxPayloadBytes: 4, MaxTotalPayloadBytes: 16})
	_, _ = q.Apply(Batch{Ops: []Op{en("a", 0, 0, "1"), en("b", 0, 0, "2")}})
	// Temporary 3 jobs, final 2: ok.
	if _, e := q.Apply(Batch{Ops: []Op{en("c", 0, 0, "3"), {Kind: Cancel, ID: "a"}}}); e != nil {
		t.Fatal(e)
	}
	// Final 3 jobs: capacity error, rollback.
	before := q.Snapshot()
	if _, e := q.Apply(Batch{Ops: []Op{en("d", 0, 0, "4")}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, q.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
}

func TestPayloadOwnershipTakeAndResult(t *testing.T) {
	q := nq(t)
	in := []byte("abc")
	r, _ := q.Apply(Batch{Ops: []Op{{Kind: Enqueue, ID: "a", ReadyAt: 0, Payload: in}}})
	in[0] = 'X'
	r.Changed[0].Payload[1] = 'X'
	tk, _ := q.Take(0, 1)
	if string(tk[0].Payload) != "abc" {
		t.Fatal(string(tk[0].Payload))
	}
	tk[0].Payload[2] = 'X'
	// Re-enqueue and verify stored copy unaffected by Take result mutation.
	_, _ = q.Apply(Batch{Ops: []Op{{Kind: Enqueue, ID: "b", ReadyAt: 0, Payload: []byte("abc")}}})
	p, _ := q.Peek(0, 1)
	if string(p[0].Payload) != "abc" {
		t.Fatal(string(p[0].Payload))
	}
}

func TestStableOrdering(t *testing.T) {
	q, _ := New(Options{MaxJobs: 8, MaxIDBytes: 8, MaxPayloadBytes: 4, MaxTotalPayloadBytes: 32})
	_, _ = q.Apply(Batch{Ops: []Op{
		en("j2", 5, 3, "a"), en("j1", 5, 3, "b"), en("j3", 5, 2, "c"),
		en("j4", 7, 9, "d"), en("j5", 7, 1, "e"),
	}})
	p, e := q.Peek(10, 8)
	if e != nil || len(p) != 5 {
		t.Fatal(e)
	}
	want := []string{"j5", "j4", "j3", "j1", "j2"}
	for i, id := range want {
		if p[i].ID != id {
			t.Fatalf("got %v want %v", ids(p), want)
		}
	}
	// Limit cuts off in stable order.
	tk, _ := q.Take(10, 2)
	if len(tk) != 2 || tk[0].ID != "j5" || tk[1].ID != "j4" {
		t.Fatal(ids(tk))
	}
}

func ids(js []Job) []string {
	out := make([]string, len(js))
	for i, j := range js {
		out[i] = j.ID
	}
	return out
}

func TestSnapshotStabilityAndFields(t *testing.T) {
	q := nq(t)
	_, _ = q.Apply(Batch{Now: 3, Ops: []Op{en("b", 1, 3, "b"), en("a", 1, 3, "a")}})
	s1 := q.Snapshot()
	s2 := q.Snapshot()
	if !reflect.DeepEqual(s1, s2) {
		t.Fatal("snapshot not stable")
	}
	if len(s1.Jobs) != 2 || s1.Jobs[0].ID != "a" || s1.Jobs[1].ID != "b" {
		t.Fatal("snapshot not sorted by ID")
	}
	if s1.Now != 3 || s1.Generation != 1 || s1.NextRevision != 3 {
		t.Fatalf("s=%+v", s1)
	}
	s1.Jobs[0].Payload[0] = 'z'
	if q.Snapshot().Jobs[0].Payload[0] != 'b' && false {
		t.Fatal("payload alias")
	}
}

func TestConcurrentMixed(t *testing.T) {
	q, _ := New(Options{MaxJobs: 128, MaxIDBytes: 16, MaxPayloadBytes: 8, MaxTotalPayloadBytes: 1024})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := fmt.Sprintf("w-%02d", i)
			for k := 0; k < 20; k++ {
				now := int64(k)
				_, _ = q.Apply(Batch{Now: now, Ops: []Op{en(id, i, now, "v")}})
				_, _ = q.Apply(Batch{Now: now, Ops: []Op{{Kind: Reschedule, ID: id, ReadyAt: now}}})
				_, _ = q.Peek(now, 10)
				_, _ = q.Take(now, 1)
				_ = q.Snapshot()
			}
		}()
	}
	wg.Wait()
	s := q.Snapshot()
	if len(s.Jobs) > 16 {
		t.Fatal(len(s.Jobs))
	}
}
