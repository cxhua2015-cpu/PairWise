package expirytable234

import (
	"errors"
	"sync"
	"testing"
)

func TestBoundaryExtra(t *testing.T) {
	if _, e := New(Options{MaxEntries: 0, MaxKeyBytes: 1}); e != ErrInvalidOptions {
		t.Fatal(e)
	}
	if _, e := New(Options{MaxEntries: 1, MaxKeyBytes: 0}); e != ErrInvalidOptions {
		t.Fatal(e)
	}
	x := table(t)
	// Closed-bound expiry inside Apply: entry expiring at Now is removed.
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 5}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}}); e != nil {
		t.Fatal(e)
	}
	r, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "c", 9}}})
	if e != nil {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if len(s.Entries) != 2 || s.Entries[0].Key != "b" || r.Revision != 3 {
		t.Fatalf("%+v %+v", r, s)
	}
	// Touch/Delete on missing keys.
	if _, e := x.Apply(Batch{Now: 6, Ops: []Op{{Touch, "zz", 9}}}); e != ErrNotFound {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 6, Ops: []Op{{Delete, "zz", 0}}}); e != ErrNotFound {
		t.Fatal(e)
	}
	// Capacity rollback: nothing changes on failure.
	before := x.Snapshot()
	_, e = x.Apply(Batch{Now: 6, Ops: []Op{{Put, "d", 9}, {Put, "e", 9}, {Put, "f", 9}}})
	if e != ErrCapacity || x.Snapshot().Now != before.Now || len(x.Snapshot().Entries) != len(before.Entries) {
		t.Fatal(e)
	}
	// Time rollback: failed batch must not advance now or revision.
	_, e = x.Apply(Batch{Now: 7, Ops: []Op{{Touch, "nope", 9}}})
	if e != ErrNotFound || x.Snapshot().Now != 5 {
		t.Fatal(e)
	}
	// Expire closed bound and monotonic time.
	gone, e := x.Expire(6)
	if e != nil || len(gone) != 1 || gone[0].Key != "b" {
		t.Fatal(e, gone)
	}
	if _, e := x.Expire(5); e != ErrTime {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); e != ErrInvalidInput {
		t.Fatal(e)
	}
	// Key charset and length limits.
	for _, k := range []string{"", "A", "a b", "a.b", "toolongkey"} {
		if e := x.ValidateBatch(Batch{Ops: []Op{{Put, k, 9}}}); e != ErrInvalidInput {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	if e := x.ValidateBatch(Batch{Ops: []Op{{Kind(99), "a", 9}}}); e != ErrInvalidInput {
		t.Fatal(e)
	}
	// Empty batch: generation unchanged.
	g := x.Stats().Generation
	if _, e := x.Apply(Batch{Now: 7}); e != nil {
		t.Fatal(e)
	}
	if x.Stats().Generation != g {
		t.Fatal("empty batch bumped generation")
	}
}

func TestConcurrentExtra(t *testing.T) {
	x, _ := New(Options{MaxEntries: 128, MaxKeyBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a'+i%8)) + string(rune('0'+i/8))
			for n := int64(1); n <= 50; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_ = x.Stats()
				_ = x.Snapshot()
				if n%10 == 0 {
					_, _ = x.Expire(n)
					_, _ = x.Clone()
				}
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	st := x.Stats()
	if st.Entries != len(s.Entries) || st.Now != s.Now || st.NextRevision != s.NextRevision {
		t.Fatalf("stats/snapshot diverge: %+v vs %+v", st, s)
	}
	// Clone independence under continued mutation.
	c, _ := x.Clone()
	_, _ = c.Apply(Batch{Now: 1000, Ops: []Op{{Put, "z", 2000}}})
	if x.Snapshot().Now == 1000 {
		t.Fatal("clone mutation leaked into original")
	}
}
