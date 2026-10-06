package balanceledger207

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func opt() Options { return Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100} }

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 8, 100}, {8, 0, 100}, {8, 8, 0}, {-1, 8, 100}, {8, -1, 100}, {8, 8, -5},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("opts %+v: %v", o, e)
		}
	}
}

func TestInvalidInput(t *testing.T) {
	l, _ := New(opt())
	bad := []Batch{
		{Ops: []Op{{Kind: 0, Name: "a"}}},
		{Ops: []Op{{Kind: 99, Name: "a"}}},
		{Ops: []Op{{Add, "", 1, 0}}},
		{Ops: []Op{{Add, "Upper", 1, 0}}},
		{Ops: []Op{{Add, "has space", 1, 0}}},
		{Ops: []Op{{Add, "waytoolongname", 1, 0}}},
		{Ops: []Op{{Add, "a.b", 1, 0}}},
	}
	for i, b := range bad {
		if _, e := l.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
	}
	// Structural validation happens before any state read: unknown kind on a
	// missing name must still be ErrInvalidInput, not ErrNotFound.
	if _, e := l.Apply(Batch{Ops: []Op{{Delete, "missing", 0, 0}, {Kind: 7, Name: "x"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Valid charset boundary: digits, hyphen, underscore, exactly max bytes.
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a1-_z9-0", 0, 1}}}); e != nil {
		t.Fatal(e)
	}
}

func TestOverflowAndAbsLimit(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); e != nil {
		t.Fatal(e)
	}
	// Overflow must be detected before arithmetic, not after wraparound.
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64}, {Add, "b", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// Failed batches roll back: "a" still MaxInt64, "b" absent.
	s := l.Snapshot()
	if len(s.Accounts) != 1 || s.Accounts[0].Value != math.MaxInt64 {
		t.Fatal(s)
	}
	// Absolute limit enforced on Add and Set.
	l2, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 10})
	if _, e := l2.Apply(Batch{Ops: []Op{{Set, "x", 0, 11}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l2.Apply(Batch{Ops: []Op{{Add, "x", 6, 0}, {Add, "x", 5, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l2.Apply(Batch{Ops: []Op{{Add, "x", -10, 0}}}); e != nil {
		t.Fatal(e) // -10 is exactly at the limit
	}
}

func TestCapacityRollbackAndGeneration(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 2, MaxNameBytes: 8, MaxAbsValue: 100})
	r1, _ := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	if r1.Generation != 1 {
		t.Fatal(r1)
	}
	// Transiently exceeds capacity inside the batch but fine at the end.
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, 2}, {Set, "c", 0, 3}, {Delete, "c", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
	// Permanently exceeds capacity: whole batch rolls back, generation unchanged.
	g := l.Snapshot().Generation
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "c", 0, 3}, {Set, "d", 0, 4}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := l.Snapshot()
	if s.Generation != g || len(s.Accounts) != 2 {
		t.Fatal(s)
	}
	// Empty batch succeeds without bumping the generation.
	r, e := l.Apply(Batch{})
	if e != nil || r.Generation != g {
		t.Fatal(r, e)
	}
}

func TestRevisionSequence(t *testing.T) {
	l, _ := New(opt())
	r, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}, {Delete, "a", 0, 0}, {Set, "b", 0, 2}}})
	if e != nil || r.Revision != 2 { // Delete consumes no revision
		t.Fatal(r, e)
	}
	s := l.Snapshot()
	if s.NextRevision != 3 || s.Accounts[0].Revision != 2 {
		t.Fatal(s)
	}
}

func TestTopOrdering(t *testing.T) {
	l, _ := New(opt())
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -1},
	}})
	top, e := l.Top(3)
	if e != nil || len(top) != 3 {
		t.Fatal(top, e)
	}
	want := []string{"c", "a", "b"} // value desc, name asc on ties
	for i, w := range want {
		if top[i].Name != w {
			t.Fatal(top)
		}
	}
	if _, e := l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if all, _ := l.Top(100); len(all) != 4 {
		t.Fatal(all)
	}
	// Returned slices are isolated from internal state.
	top[0].Value = -999
	if again, _ := l.Top(1); again[0].Value != 9 {
		t.Fatal(again)
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 32, MaxNameBytes: 8, MaxAbsValue: 1 << 40})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a' + i))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
				_, _ = l.Apply(Batch{Ops: []Op{{Set, k, 0, int64(j)}}})
				_, _ = l.Top(4)
				_ = l.Snapshot()
			}
		}()
	}
	w.Wait()
	s := l.Snapshot()
	if len(s.Accounts) != 16 {
		t.Fatal(len(s.Accounts))
	}
	// Revisions are dense: 16 keys * 100 revision-allocating ops.
	if s.NextRevision != 16*100+1 {
		t.Fatal(s.NextRevision)
	}
}
