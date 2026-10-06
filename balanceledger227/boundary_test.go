package balanceledger227

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestValidationBoundaries(t *testing.T) {
	l := led(t)
	cases := []struct {
		name string
		b    Batch
		want error
	}{
		{"unknown kind", Batch{Ops: []Op{{Kind: 99, Name: "a"}}}, ErrInvalidInput},
		{"empty name", Batch{Ops: []Op{{Add, "", 1, 0}}}, ErrInvalidInput},
		{"uppercase name", Batch{Ops: []Op{{Add, "A", 1, 0}}}, ErrInvalidInput},
		{"space name", Batch{Ops: []Op{{Add, "a b", 1, 0}}}, ErrInvalidInput},
		{"long name", Batch{Ops: []Op{{Add, "abcdefghi", 1, 0}}}, ErrInvalidInput},
		{"zero delta", Batch{Ops: []Op{{Add, "a", 0, 0}}}, ErrInvalidInput},
		{"add extra value", Batch{Ops: []Op{{Add, "a", 1, 1}}}, ErrInvalidInput},
		{"set extra delta", Batch{Ops: []Op{{Set, "a", 1, 1}}}, ErrInvalidInput},
		{"delete extra fields", Batch{Ops: []Op{{Delete, "a", 0, 1}}}, ErrInvalidInput},
		{"set over limit", Batch{Ops: []Op{{Set, "a", 0, 21}}}, ErrValue},
		{"set minint", Batch{Ops: []Op{{Set, "a", 0, math.MinInt64}}}, ErrValue},
		{"add minint delta", Batch{Ops: []Op{{Add, "a", math.MinInt64, 0}}}, ErrValue},
		{"delta over limit", Batch{Ops: []Op{{Add, "a", 21, 0}}}, ErrValue},
	}
	for _, c := range cases {
		if got := l.ValidateBatch(c.b); !errors.Is(got, c.want) {
			t.Errorf("%s: ValidateBatch=%v want %v", c.name, got, c.want)
		}
		if _, got := l.Apply(c.b); !errors.Is(got, c.want) {
			t.Errorf("%s: Apply=%v want %v", c.name, got, c.want)
		}
	}
	if got := l.ValidateBatch(Batch{Ops: []Op{{Add, "ok-1_2", 1, 0}}}); got != nil {
		t.Fatal(got)
	}
	if l.Stats().Accounts != 0 {
		t.Fatal("validation must be side-effect free")
	}
}

func TestOverflowAndAbsLimit(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal("expected overflow detection, got", e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64 + 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Add, "b", -1, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal("expected negative overflow detection, got", e)
	}
	s := l.Snapshot()
	if s.Accounts[0].Value != math.MaxInt64 || s.Accounts[1].Value != math.MinInt64+1 {
		t.Fatal("overflowing batch must roll back", s)
	}
}

func TestGenerationAndRevisionClocks(t *testing.T) {
	l := led(t)
	r, e := l.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatal("empty batch must not advance clocks", r, e)
	}
	r, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}, {Delete, "a", 0, 0}, {Set, "b", 0, 2}}})
	if r.Generation != 1 || r.Revision != 2 || len(r.Changed) != 1 || r.Changed[0].Name != "b" {
		t.Fatal(r)
	}
	if _, e := l.Apply(Batch{Ops: []Op{{Delete, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	z := l.Stats()
	if z.Generation != 1 || z.NextRevision != 3 || z.Accounts != 1 {
		t.Fatal("failed batch must not advance clocks", z)
	}
}

func TestTopOrderingAndIsolation(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 8, MaxNameBytes: 8, MaxAbsValue: 100})
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "b", 0, 5}, {Set, "a", 0, 5}, {Set, "c", 0, 9}, {Set, "d", 0, -1}}})
	top, e := l.Top(3)
	if e != nil || len(top) != 3 || top[0].Name != "c" || top[1].Name != "a" || top[2].Name != "b" {
		t.Fatal(top, e)
	}
	top[0].Value = -999
	if l.Snapshot().Accounts[2].Value != 9 {
		t.Fatal("returned slices must be isolated from internal state")
	}
	if _, e := l.Top(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if all, _ := l.Top(100); len(all) != 4 {
		t.Fatal(all)
	}
}

func TestCloneIndependence(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 7}}})
	c, e := l.Clone()
	if e != nil {
		t.Fatal(e)
	}
	if z := c.Stats(); z.Generation != 1 || z.NextRevision != 2 {
		t.Fatal("clone must preserve logical clocks", z)
	}
	_, _ = c.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}}})
	if l.Stats().Accounts != 1 || c.Stats().Accounts != 0 {
		t.Fatal("clone must be fully independent")
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 128, MaxNameBytes: 8, MaxAbsValue: 1000000})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a'+i%26)) + string(rune('0'+i/26))
			for j := 0; j < 50; j++ {
				_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
				_ = l.ValidateBatch(Batch{Ops: []Op{{Set, k, 0, 1}}})
				_, _ = l.Top(4)
				_ = l.Stats()
				_ = l.Snapshot()
				if j%10 == 0 {
					_, _ = l.Clone()
				}
			}
		}()
	}
	w.Wait()
	z := l.Stats()
	if z.Generation != uint64(z.NextRevision-1) || z.Accounts != 32 {
		t.Fatalf("inconsistent clocks after concurrency: %+v", z)
	}
}
