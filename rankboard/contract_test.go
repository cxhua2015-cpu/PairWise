package rankboard

import (
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
)

func board(t *testing.T) *Board {
	t.Helper()
	b, e := New(Options{MaxItems: 4, MaxIDBytes: 12, MaxAbsScore: 100})
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func TestValidationAndOrder(t *testing.T) {
	b := board(t)
	before := b.Snapshot()
	_, e := b.Apply(Batch{Ops: []Op{{Kind: Add, ID: "bad?", Delta: 1}}})
	if !errors.Is(e, ErrInvalidInput) || !reflect.DeepEqual(before, b.Snapshot()) {
		t.Fatal(e)
	}
	x, e := b.Apply(Batch{Ops: []Op{{Kind: Add, ID: "a", Delta: 2}, {Kind: Add, ID: "a", Delta: 3}}})
	if e != nil || x.Revision != 2 || len(x.Changed) != 1 || x.Changed[0].Score != 5 {
		t.Fatal(e)
	}
}
func TestTopStable(t *testing.T) {
	b := board(t)
	_, _ = b.Apply(Batch{Ops: []Op{{Kind: Set, ID: "b", Score: 5}, {Kind: Set, ID: "a", Score: 5}, {Kind: Set, ID: "c", Score: 8}}})
	x, e := b.Top(2)
	if e != nil || len(x) != 2 || x[0].ID != "c" || x[1].ID != "a" {
		t.Fatalf("%+v %v", x, e)
	}
}
func TestRollbackAndFinalCapacity(t *testing.T) {
	b, _ := New(Options{MaxItems: 1, MaxIDBytes: 8, MaxAbsScore: 10})
	_, _ = b.Apply(Batch{Ops: []Op{{Kind: Set, ID: "a", Score: 1}}})
	_, e := b.Apply(Batch{Ops: []Op{{Kind: Delete, ID: "a"}, {Kind: Set, ID: "b", Score: 2}}})
	if e != nil || b.Snapshot().Items[0].ID != "b" {
		t.Fatal(e)
	}
	s := b.Snapshot()
	_, e = b.Apply(Batch{Ops: []Op{{Kind: Add, ID: "b", Delta: 9}}})
	if !errors.Is(e, ErrScore) || !reflect.DeepEqual(s, b.Snapshot()) {
		t.Fatal(e)
	}
}
func TestOverflow(t *testing.T) {
	b, _ := New(Options{MaxItems: 2, MaxIDBytes: 8, MaxAbsScore: math.MaxInt64})
	_, _ = b.Apply(Batch{Ops: []Op{{Kind: Set, ID: "a", Score: math.MaxInt64}}})
	if _, e := b.Apply(Batch{Ops: []Op{{Kind: Add, ID: "a", Delta: 1}}}); !errors.Is(e, ErrScore) {
		t.Fatal(e)
	}
}
func TestDeleteMissing(t *testing.T) {
	b := board(t)
	if _, e := b.Apply(Batch{Ops: []Op{{Kind: Delete, ID: "x"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}
func TestConcurrent(t *testing.T) {
	b, _ := New(Options{MaxItems: 64, MaxIDBytes: 8, MaxAbsScore: 100})
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := string(rune('a' + i))
			_, _ = b.Apply(Batch{Ops: []Op{{Kind: Add, ID: id, Delta: int64(i + 1)}}})
			_, _ = b.Top(64)
			_ = b.Snapshot()
		}()
	}
	wg.Wait()
	if len(b.Snapshot().Items) != 24 {
		t.Fatal(len(b.Snapshot().Items))
	}
}
