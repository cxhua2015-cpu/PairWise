package expirytable269

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func TestBoundaryValidation(t *testing.T) {
	if _, e := New(Options{0, 1}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	if _, e := New(Options{1, 0}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	x := table(t)
	cases := []Batch{
		{Now: -1, Ops: []Op{{Put, "a", 1}}},
		{Ops: []Op{{Put, "", 1}}},
		{Ops: []Op{{Put, "UPPER", 1}}},
		{Ops: []Op{{Put, "a b", 1}}},
		{Ops: []Op{{Put, "toolongkey", 1}}},
		{Ops: []Op{{Kind(0), "a", 1}}},
		{Ops: []Op{{Kind(9), "a", 1}}},
		{Ops: []Op{{Put, "a", 0}}},
		{Now: 5, Ops: []Op{{Touch, "a", 5}}},
	}
	for i, b := range cases {
		if e := x.ValidateBatch(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
		if _, e := x.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, e)
		}
	}
	if e := x.ValidateBatch(Batch{Ops: []Op{{Put, "ok_1-x", 1}}}); e != nil {
		t.Fatal(e)
	}
}

func TestNotFoundAndRollback(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Touch, "ghost", 9}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Delete, "ghost", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	r, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}, {Put, "b", 3}, {Put, "c", 4}}})
	if e != nil || r.Generation != 1 || r.Revision != 3 {
		t.Fatal(e, r)
	}
	before := x.Snapshot()
	_, e = x.Apply(Batch{Now: 3, Ops: []Op{{Put, "d", 9}, {Put, "e", 9}, {Put, "f", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	after := x.Snapshot()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("rollback: %+v -> %+v", before, after)
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 2})
	if e != nil || r.Generation != 0 || x.Snapshot().Now != 2 {
		t.Fatal(e, r)
	}
	r, e = x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 9}, {Delete, "a", 0}}})
	if e != nil || r.Generation != 1 {
		t.Fatal(e, r)
	}
}

func TestExpireMonotonic(t *testing.T) {
	x := table(t)
	if _, e := x.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	_, _ = x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 9}}})
	if _, e := x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	gone, e := x.Expire(9)
	if e != nil || len(gone) != 1 {
		t.Fatal(e, gone)
	}
}

func TestConcurrentMixed(t *testing.T) {
	x, _ := New(Options{MaxEntries: 128, MaxKeyBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a'+i%8)) + string(rune('0'+i/8))
			for n := int64(1); n <= 20; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_ = x.Stats()
				_ = x.Snapshot()
				_ = x.ValidateBatch(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
			}
		}()
	}
	w.Add(1)
	go func() {
		defer w.Done()
		for n := 0; n < 10; n++ {
			c, err := x.Clone()
			if err != nil {
				t.Error(err)
				return
			}
			_, _ = c.Expire(1 << 40)
		}
	}()
	w.Wait()
	s := x.Stats()
	if s.Entries > 16 || s.Now != 20 {
		t.Fatalf("%+v", s)
	}
}
