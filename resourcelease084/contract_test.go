package resourcelease084

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func table(t *testing.T) *Table {
	t.Helper()
	x, e := New(Options{MaxEntries: 3, MaxKeyBytes: 8})
	if e != nil {
		t.Fatal(e)
	}
	return x
}
func TestBoundaryAndTouch(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Ops: []Op{{Put, "a", 3}, {Touch, "a", 5}}})
	if e != nil || r.Revision != 2 {
		t.Fatal(e)
	}
	gone, e := x.Expire(5)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
}
func TestRollbackTime(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 4, Ops: []Op{{Put, "a", 8}}})
	b := x.Snapshot()
	_, e := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "bad?", 9}}})
	if !errors.Is(e, ErrInvalidInput) || !reflect.DeepEqual(b, x.Snapshot()) {
		t.Fatal(e)
	}
}
func TestFinalCapacity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 2}}})
	_, e := x.Apply(Batch{Ops: []Op{{Delete, "a", 0}, {Put, "b", 3}}})
	if e != nil || x.Snapshot().Entries[0].Key != "b" {
		t.Fatal(e)
	}
}
func TestConcurrent(t *testing.T) {
	x, _ := New(Options{MaxEntries: 64, MaxKeyBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 20; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a' + i))
			_, _ = x.Apply(Batch{Ops: []Op{{Put, k, 9}}})
			_ = x.Snapshot()
		}()
	}
	w.Wait()
	if len(x.Snapshot().Entries) != 20 {
		t.Fatal(len(x.Snapshot().Entries))
	}
}
