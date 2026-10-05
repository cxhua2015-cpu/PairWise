package rankboard

import (
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
)

func TestNewInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{MaxItems: 0, MaxIDBytes: 1, MaxAbsScore: 1},
		{MaxItems: 1, MaxIDBytes: 0, MaxAbsScore: 1},
		{MaxItems: 1, MaxIDBytes: 1, MaxAbsScore: 0},
		{MaxItems: -1, MaxIDBytes: 1, MaxAbsScore: 1},
		{MaxItems: 1, MaxIDBytes: 1, MaxAbsScore: -5},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestIDValidation(t *testing.T) {
	b := board(t)
	for _, id := range []string{"", "has space", "中文", "a@b", "toolongid12345", "tab\tx"} {
		if _, e := b.Apply(Batch{Ops: []Op{{Kind: Set, ID: id, Score: 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, e)
		}
	}
	if _, e := b.Apply(Batch{Ops: []Op{{Kind: Set, ID: "a-Z_0.9/x", Score: 1}}}); e != nil {
		t.Fatal(e)
	}
}

func TestOpFieldValidation(t *testing.T) {
	b := board(t)
	cases := []Op{
		{Kind: Add, ID: "a"},
		{Kind: Add, ID: "a", Delta: 1, Score: 1},
		{Kind: Set, ID: "a", Delta: 1, Score: 1},
		{Kind: Set, ID: "a", Score: 101},
		{Kind: Set, ID: "a", Score: -101},
		{Kind: Delete, ID: "a", Delta: 1},
		{Kind: Delete, ID: "a", Score: 1},
		{Kind: Kind(0), ID: "a"},
		{Kind: Kind(99), ID: "a"},
	}
	for _, op := range cases {
		if _, e := b.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%+v: %v", op, e)
		}
	}
}

func TestEmptyBatch(t *testing.T) {
	b := board(t)
	r, e := b.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || r.Changed != nil {
		t.Fatalf("%+v %v", r, e)
	}
	if s := b.Snapshot(); s.Generation != 0 || s.NextRevision != 1 || len(s.Items) != 0 {
		t.Fatalf("%+v", s)
	}
}

func TestScoreLimitsAndUnderflow(t *testing.T) {
	b, _ := New(Options{MaxItems: 4, MaxIDBytes: 8, MaxAbsScore: math.MaxInt64})
	if _, e := b.Apply(Batch{Ops: []Op{{Kind: Set, ID: "a", Score: math.MinInt64 + 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := b.Apply(Batch{Ops: []Op{{Kind: Add, ID: "a", Delta: -1}}}); !errors.Is(e, ErrScore) {
		t.Fatal(e)
	}
	b2, _ := New(Options{MaxItems: 4, MaxIDBytes: 8, MaxAbsScore: 10})
	if _, e := b2.Apply(Batch{Ops: []Op{{Kind: Add, ID: "a", Delta: -11}}}); !errors.Is(e, ErrScore) {
		t.Fatal(e)
	}
	if _, e := b2.Apply(Batch{Ops: []Op{{Kind: Add, ID: "a", Delta: 10}}}); e != nil {
		t.Fatal(e)
	}
	if got := b2.Snapshot().Items[0].Score; got != 10 {
		t.Fatal(got)
	}
}

func TestCapacityRollbackRevisions(t *testing.T) {
	b, _ := New(Options{MaxItems: 1, MaxIDBytes: 8, MaxAbsScore: 100})
	if _, e := b.Apply(Batch{Ops: []Op{{Kind: Set, ID: "a", Score: 1}}}); e != nil {
		t.Fatal(e)
	}
	s := b.Snapshot()
	_, e := b.Apply(Batch{Ops: []Op{{Kind: Set, ID: "b", Score: 2}, {Kind: Set, ID: "c", Score: 3}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(s, b.Snapshot()) {
		t.Fatal("state changed after rollback")
	}
	r, e := b.Apply(Batch{Ops: []Op{{Kind: Set, ID: "b", Score: 2}, {Kind: Delete, ID: "b"}}})
	if e != nil || len(r.Changed) != 0 {
		t.Fatalf("%+v %v", r, e)
	}
	if b.Snapshot().Generation != s.Generation+1 {
		t.Fatal("generation should increment once")
	}
}

func TestDeleteThenReaddInBatch(t *testing.T) {
	b := board(t)
	r, e := b.Apply(Batch{Ops: []Op{
		{Kind: Set, ID: "a", Score: 5},
		{Kind: Delete, ID: "a"},
		{Kind: Add, ID: "a", Delta: 2},
	}})
	if e != nil || len(r.Changed) != 1 || r.Changed[0].Score != 2 {
		t.Fatalf("%+v %v", r, e)
	}
}

func TestTopLimitAndIsolation(t *testing.T) {
	b := board(t)
	for _, l := range []int{0, -1, 1001} {
		if _, e := b.Top(l); !errors.Is(e, ErrInvalidInput) {
			t.Fatal(l)
		}
	}
	_, _ = b.Apply(Batch{Ops: []Op{{Kind: Set, ID: "a", Score: 1}, {Kind: Set, ID: "b", Score: 2}}})
	top, _ := b.Top(1000)
	if len(top) != 2 || top[0].ID != "b" {
		t.Fatalf("%+v", top)
	}
	top[0].Score = 999
	snap := b.Snapshot()
	snap.Items[0].Score = 999
	if b.Snapshot().Items[0].Score == 999 {
		t.Fatal("returned slices must be isolated")
	}
}

func TestConcurrentMixed(t *testing.T) {
	b, _ := New(Options{MaxItems: 1000, MaxIDBytes: 8, MaxAbsScore: math.MaxInt64})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := string(rune('a'+i%26)) + string(rune('0'+i/26))
			for j := 0; j < 50; j++ {
				_, _ = b.Apply(Batch{Ops: []Op{{Kind: Add, ID: id, Delta: 1}}})
				_, _ = b.Top(10)
				_ = b.Snapshot()
			}
		}()
	}
	wg.Wait()
	s := b.Snapshot()
	var total int64
	for _, it := range s.Items {
		total += it.Score
	}
	if total != 32*50 {
		t.Fatalf("total=%d", total)
	}
	if s.NextRevision != 32*50+1 {
		t.Fatalf("next=%d", s.NextRevision)
	}
}
