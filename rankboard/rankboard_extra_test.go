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
		{MaxItems: 1, MaxIDBytes: 1, MaxAbsScore: -1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestValidationErrors(t *testing.T) {
	b := board(t)
	cases := []Op{
		{Kind: Add, ID: "", Delta: 1},
		{Kind: Add, ID: "toolongidtoolong", Delta: 1},
		{Kind: Add, ID: "bad id", Delta: 1},
		{Kind: Add, ID: "a", Delta: 0},
		{Kind: Add, ID: "a", Delta: 1, Score: 1},
		{Kind: Set, ID: "a", Delta: 1, Score: 1},
		{Kind: Set, ID: "a", Score: 101},
		{Kind: Set, ID: "a", Score: -101},
		{Kind: Delete, ID: "a", Delta: 1},
		{Kind: Delete, ID: "a", Score: 1},
		{Kind: Kind(0), ID: "a"},
		{Kind: Kind(9), ID: "a"},
	}
	for _, op := range cases {
		if _, e := b.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%+v: %v", op, e)
		}
	}
	if s := b.Snapshot(); s.Generation != 0 || len(s.Items) != 0 {
		t.Fatalf("state mutated: %+v", s)
	}
}

func TestEmptyBatch(t *testing.T) {
	b := board(t)
	x, e := b.Apply(Batch{})
	if e != nil || !reflect.DeepEqual(x, Result{}) {
		t.Fatalf("%+v %v", x, e)
	}
	if s := b.Snapshot(); s.Generation != 0 || s.NextRevision != 1 {
		t.Fatalf("%+v", s)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	b := board(t)
	x, _ := b.Apply(Batch{Ops: []Op{{Kind: Set, ID: "a", Score: 1}, {Kind: Set, ID: "b", Score: 2}}})
	if x.Generation != 1 || x.Revision != 2 {
		t.Fatalf("%+v", x)
	}
	x, _ = b.Apply(Batch{Ops: []Op{{Kind: Delete, ID: "a"}}})
	if x.Generation != 2 || x.Revision != 2 || len(x.Changed) != 0 {
		t.Fatalf("%+v", x)
	}
	s := b.Snapshot()
	if s.Generation != 2 || s.NextRevision != 3 {
		t.Fatalf("%+v", s)
	}
	// Failed batch must not advance generation or revision.
	if _, e := b.Apply(Batch{Ops: []Op{{Kind: Set, ID: "c", Score: 1}, {Kind: Delete, ID: "zz"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if got := b.Snapshot(); !reflect.DeepEqual(s, got) {
		t.Fatalf("%+v != %+v", got, s)
	}
}

func TestChangedSortedAndSurviving(t *testing.T) {
	b := board(t)
	x, e := b.Apply(Batch{Ops: []Op{
		{Kind: Set, ID: "c", Score: 1},
		{Kind: Set, ID: "a", Score: 2},
		{Kind: Set, ID: "b", Score: 3},
		{Kind: Delete, ID: "c"},
		{Kind: Add, ID: "a", Delta: 1},
	}})
	if e != nil {
		t.Fatal(e)
	}
	if len(x.Changed) != 2 || x.Changed[0].ID != "a" || x.Changed[1].ID != "b" {
		t.Fatalf("%+v", x.Changed)
	}
	if x.Changed[0].Score != 3 || x.Changed[0].Revision != 4 {
		t.Fatalf("%+v", x.Changed[0])
	}
}

func TestCapacityOnlyAtEnd(t *testing.T) {
	b, _ := New(Options{MaxItems: 2, MaxIDBytes: 8, MaxAbsScore: 100})
	// Temporarily exceed capacity mid-batch, then delete back under.
	_, e := b.Apply(Batch{Ops: []Op{
		{Kind: Set, ID: "a", Score: 1},
		{Kind: Set, ID: "b", Score: 2},
		{Kind: Set, ID: "c", Score: 3},
		{Kind: Delete, ID: "a"},
	}})
	if e != nil || len(b.Snapshot().Items) != 2 {
		t.Fatalf("%v %+v", e, b.Snapshot())
	}
	// Exceeding at the end fails and rolls back fully.
	s := b.Snapshot()
	_, e = b.Apply(Batch{Ops: []Op{{Kind: Set, ID: "d", Score: 4}, {Kind: Set, ID: "e", Score: 5}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(s, b.Snapshot()) {
		t.Fatalf("%v %+v", e, b.Snapshot())
	}
}

func TestScoreArithmeticEdges(t *testing.T) {
	b, _ := New(Options{MaxItems: 4, MaxIDBytes: 8, MaxAbsScore: math.MaxInt64})
	if _, e := b.Apply(Batch{Ops: []Op{{Kind: Set, ID: "a", Score: math.MinInt64 + 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := b.Apply(Batch{Ops: []Op{{Kind: Add, ID: "a", Delta: -1}}}); !errors.Is(e, ErrScore) {
		t.Fatal(e)
	}
	if _, e := b.Apply(Batch{Ops: []Op{{Kind: Add, ID: "b", Delta: math.MinInt64}}}); !errors.Is(e, ErrScore) {
		t.Fatal(e)
	}
	// Limit enforced after arithmetic, not on delta alone.
	b2, _ := New(Options{MaxItems: 4, MaxIDBytes: 8, MaxAbsScore: 10})
	if _, e := b2.Apply(Batch{Ops: []Op{{Kind: Add, ID: "a", Delta: 50}}}); !errors.Is(e, ErrScore) {
		t.Fatal(e)
	}
	if _, e := b2.Apply(Batch{Ops: []Op{{Kind: Add, ID: "a", Delta: 10}, {Kind: Add, ID: "a", Delta: -20}}}); e != nil {
		t.Fatal(e)
	}
	if got := b2.Snapshot().Items[0].Score; got != -10 {
		t.Fatal(got)
	}
}

func TestTopLimitsAndIsolation(t *testing.T) {
	b := board(t)
	if _, e := b.Top(0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := b.Top(1001); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	_, _ = b.Apply(Batch{Ops: []Op{{Kind: Set, ID: "a", Score: 1}, {Kind: Set, ID: "b", Score: 2}}})
	top, _ := b.Top(1000)
	if len(top) != 2 || top[0].ID != "b" {
		t.Fatalf("%+v", top)
	}
	// Mutating returned slices must not affect the board.
	top[0].Score = -999
	snap := b.Snapshot()
	snap.Items[0].Score = -999
	if b.Snapshot().Items[0].Score != 1 {
		t.Fatal("returned slices alias internal state")
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
		t.Fatalf("total=%d items=%d", total, len(s.Items))
	}
	if s.Generation != 32*50 || s.NextRevision != 32*50+1 {
		t.Fatalf("%+v", s)
	}
}
