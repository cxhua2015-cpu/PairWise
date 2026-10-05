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
	if _, e := q.Take(0, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatalf("limit 0: %v", e)
	}
	if _, e := q.Take(0, 1001); !errors.Is(e, ErrInvalidInput) {
		t.Fatalf("limit 1001: %v", e)
	}
	if _, e := q.Peek(0, 1); e != nil {
		t.Fatalf("peek at 0: %v", e)
	}
	if _, e := q.Apply(Batch{Now: 0}); e != nil {
		t.Fatalf("equal now ok: %v", e)
	}
	if q.Snapshot().Now != 0 {
		t.Fatal("now should stay 0")
	}
}

func TestSequentialBatchOrder(t *testing.T) {
	q := nq(t)
	// cancel-then-enqueue same ID, repeated reschedule in one batch
	r, e := q.Apply(Batch{Now: 1, Ops: []Op{
		en("a", 1, 5, "x"),
		{Kind: Cancel, ID: "a"},
		en("a", 9, 2, "yy"),
		{Kind: Reschedule, ID: "a", Priority: 3, ReadyAt: 4},
		{Kind: Reschedule, ID: "a", Priority: 7, ReadyAt: 6},
	}})
	if e != nil {
		t.Fatal(e)
	}
	if r.Revision != 4 || len(r.Changed) != 1 {
		t.Fatalf("r=%+v", r)
	}
	j := r.Changed[0]
	if j.Priority != 7 || j.ReadyAt != 6 || j.Revision != 4 || string(j.Payload) != "yy" {
		t.Fatalf("j=%+v", j)
	}
	// enqueue existing -> conflict, whole batch rolled back
	before := q.Snapshot()
	_, e = q.Apply(Batch{Now: 2, Ops: []Op{en("b", 1, 2, "b"), en("a", 1, 2, "z")}})
	if !errors.Is(e, ErrConflict) || !reflect.DeepEqual(before, q.Snapshot()) {
		t.Fatalf("e=%v", e)
	}
}

func TestRevisionRollbackOnCapacity(t *testing.T) {
	q := nq(t)
	r, e := q.Apply(Batch{Now: 1, Ops: []Op{en("a", 1, 1, "x")}})
	if e != nil || r.Revision != 1 {
		t.Fatalf("r=%+v e=%v", r, e)
	}
	// over MaxJobs: revisions must not be consumed
	_, e = q.Apply(Batch{Now: 2, Ops: []Op{en("b", 1, 2, "x"), en("c", 1, 2, "x"), en("d", 1, 2, "x"), en("e", 1, 2, "x")}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatalf("e=%v", e)
	}
	if s := q.Snapshot(); s.NextRevision != 2 || s.Now != 1 || s.Generation != 1 {
		t.Fatalf("s=%+v", s)
	}
	// over total payload bytes
	_, e = q.Apply(Batch{Now: 2, Ops: []Op{en("b", 1, 2, "12345678"), en("c", 1, 2, "12345678")}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatalf("e=%v", e)
	}
	if s := q.Snapshot(); s.NextRevision != 2 {
		t.Fatalf("revision consumed: %+v", s)
	}
}

func TestFinalCapacityTransientOverflow(t *testing.T) {
	q, e := New(Options{MaxJobs: 2, MaxIDBytes: 8, MaxPayloadBytes: 8, MaxTotalPayloadBytes: 8})
	if e != nil {
		t.Fatal(e)
	}
	_, _ = q.Apply(Batch{Ops: []Op{en("a", 0, 0, "1234"), en("b", 0, 0, "1234")}})
	// transiently 3 jobs / 12 bytes, final 2 jobs / 8 bytes: must succeed
	r, e := q.Apply(Batch{Now: 1, Ops: []Op{
		en("c", 0, 1, "12345678"),
		{Kind: Cancel, ID: "a"},
		{Kind: Cancel, ID: "b"},
	}})
	if e != nil || r.Revision != 3 {
		t.Fatalf("r=%+v e=%v", r, e)
	}
	s := q.Snapshot()
	if len(s.Jobs) != 1 || s.Jobs[0].ID != "c" {
		t.Fatalf("s=%+v", s)
	}
	// final overflow must fail
	_, e = q.Apply(Batch{Now: 2, Ops: []Op{en("d", 0, 2, "12345678"), en("e", 0, 2, "1")}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatalf("e=%v", e)
	}
}

func TestPayloadOwnershipTakeAndResult(t *testing.T) {
	q := nq(t)
	p := []byte("abc")
	r, _ := q.Apply(Batch{Ops: []Op{{Kind: Enqueue, ID: "a", ReadyAt: 0, Payload: p}}})
	p[0] = 'z'
	r.Changed[0].Payload[1] = 'z'
	tk, e := q.Take(0, 1)
	if e != nil || string(tk[0].Payload) != "abc" {
		t.Fatalf("tk=%+v e=%v", tk, e)
	}
	tk[0].Payload[2] = 'z'
	// job is gone after take; re-enqueue and verify isolation again
	_, _ = q.Apply(Batch{Ops: []Op{{Kind: Enqueue, ID: "b", ReadyAt: 0, Payload: []byte("ok")}}})
	s := q.Snapshot()
	s.Jobs[0].Payload[0] = 'X'
	if string(q.Snapshot().Jobs[0].Payload) != "ok" {
		t.Fatal("snapshot payload not isolated")
	}
}

func TestStableOrdering(t *testing.T) {
	q, _ := New(Options{MaxJobs: 16, MaxIDBytes: 8, MaxPayloadBytes: 4, MaxTotalPayloadBytes: 64})
	_, _ = q.Apply(Batch{Ops: []Op{
		en("j2", 5, 3, "a"), en("j1", 5, 3, "a"), en("j3", 5, 2, "a"), en("j0", 9, 9, "a"),
	}})
	p, e := q.Peek(9, 10)
	if e != nil || len(p) != 4 {
		t.Fatalf("p=%+v e=%v", p, e)
	}
	want := []string{"j0", "j3", "j1", "j2"} // priority desc, readyAt asc, id asc
	for i, id := range want {
		if p[i].ID != id {
			t.Fatalf("got %v want %v", ids(p), want)
		}
	}
	// peek does not remove
	if len(q.Snapshot().Jobs) != 4 {
		t.Fatal("peek mutated queue")
	}
}

func ids(js []Job) []string {
	var out []string
	for _, j := range js {
		out = append(out, j.ID)
	}
	return out
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
				_, _ = q.Apply(Batch{Now: now, Ops: []Op{en(id, k, now, "v")}})
				_, _ = q.Apply(Batch{Now: now, Ops: []Op{{Kind: Reschedule, ID: id, Priority: k, ReadyAt: now}}})
				_, _ = q.Peek(now, 1000)
				_, _ = q.Take(now, 1)
				_ = q.Snapshot()
			}
		}()
	}
	wg.Wait()
	s := q.Snapshot()
	if len(s.Jobs) > 16 {
		t.Fatalf("too many jobs: %d", len(s.Jobs))
	}
	prev := ""
	for _, j := range s.Jobs {
		if j.ID <= prev {
			t.Fatal("snapshot not sorted by ID")
		}
		prev = j.ID
	}
}
