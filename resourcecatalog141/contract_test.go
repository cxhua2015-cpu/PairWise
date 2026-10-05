package resourcecatalog141

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func store(t *testing.T) *Store {
	t.Helper()
	s, e := New(Options{MaxRecords: 3, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func TestOrderRevision(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1")}, {Put, "a", []byte("2")}, {Put, "a", []byte("3")}}})
	if e != nil || x.Revision != 3 || len(x.Changed) != 2 || x.Changed[0].Name != "a" || x.Changed[0].Revision != 3 {
		t.Fatal(e, x)
	}
}
func TestRollback(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}})
	b := s.Snapshot()
	_, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("y")}, {Delete, "z", nil}}})
	if !errors.Is(e, ErrNotFound) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
}
func TestFinalCapacityOwnership(t *testing.T) {
	s, _ := New(Options{MaxRecords: 1, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	v := []byte("abc")
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", v}}})
	v[0] = 'z'
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "b", []byte("zz")}}})
	r, _, _ := s.Get("b")
	r.Value[0] = 'q'
	r2, _, _ := s.Get("b")
	if e != nil || string(r2.Value) != "zz" {
		t.Fatal(e)
	}
}
func TestValidation(t *testing.T) {
	s := store(t)
	b := s.Snapshot()
	_, e := s.Apply(Batch{Ops: []Op{{Put, "bad?", []byte("x")}}})
	if !errors.Is(e, ErrInvalidInput) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
}
func TestConcurrent(t *testing.T) {
	s, _ := New(Options{MaxRecords: 64, MaxNameBytes: 8, MaxValueBytes: 2, MaxTotalValueBytes: 128})
	var w sync.WaitGroup
	for i := 0; i < 24; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a' + i))
			_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
			_, _, _ = s.Get(k)
			_ = s.Snapshot()
		}()
	}
	w.Wait()
	if len(s.Snapshot().Records) != 24 {
		t.Fatal(len(s.Snapshot().Records))
	}
}
