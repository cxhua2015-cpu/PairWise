package resourceledger097

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}, {1, -1, 1}, {1, 1, -1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("opts %+v: %v", o, e)
		}
	}
}

func TestNameValidation(t *testing.T) {
	l := led(t)
	for _, n := range []string{"", "A", "a b", "a.b", "中文", "toolongname"} {
		if _, e := l.Apply(Batch{Ops: []Op{{Add, n, 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "ok_nm-1", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestUnknownKind(t *testing.T) {
	l := led(t)
	if _, e := l.Apply(Batch{Ops: []Op{{Kind(0), "a", 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Kind(9), "a", 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestOverflowAndAbs(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MinInt64}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, -math.MaxInt64}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	// State unchanged after failures.
	if got := l.Snapshot().Accounts[0].Value; got != -math.MaxInt64 {
		t.Fatal(got)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	l := led(t)
	r0, e := l.Apply(Batch{})
	if e != nil || r0.Generation != 0 || r0.Revision != 0 {
		t.Fatal(r0, e)
	}
	r1, _ := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Delete, "a", 0, 0}, {Set, "b", 0, 2}}})
	if r1.Generation != 1 || r1.Revision != 2 {
		t.Fatal(r1)
	}
	// Failed batch must not advance generation or revision.
	if _, e = l.Apply(Batch{Ops: []Op{{Set, "c", 0, 99}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
	s := l.Snapshot()
	if s.Generation != 1 || s.NextRevision != 3 {
		t.Fatal(s)
	}
}

func TestTopOrder(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{
		{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -1},
	}})
	top, e := l.Top(3)
	if e != nil || len(top) != 3 || top[0].Name != "c" || top[1].Name != "a" || top[2].Name != "b" {
		t.Fatal(top, e)
	}
	if _, e = l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	all, _ := l.Top(100)
	if len(all) != 4 || all[3].Name != "d" {
		t.Fatal(all)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	s := l.Snapshot()
	s.Accounts[0].Value = 999
	if l.Snapshot().Accounts[0].Value != 1 {
		t.Fatal("snapshot aliases internal state")
	}
	top, _ := l.Top(1)
	top[0].Value = 999
	if l.Snapshot().Accounts[0].Value != 1 {
		t.Fatal("top aliases internal state")
	}
}

func TestConcurrentStress(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 128, MaxNameBytes: 8, MaxAbsValue: 1 << 40})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a'+i%26)) + string(rune('0'+i/26))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}, {Add, k, -1, 0}, {Set, k, 0, int64(j)}}})
				_, _ = l.Top(4)
				_ = l.Snapshot()
			}
		}()
	}
	w.Wait()
	s := l.Snapshot()
	if len(s.Accounts) != 32 {
		t.Fatal(len(s.Accounts))
	}
	// 32 writers * 50 batches * 3 revision-bearing ops.
	if s.NextRevision != 32*50*3+1 {
		t.Fatal(s.NextRevision)
	}
}
