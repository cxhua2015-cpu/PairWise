package resourceledger142

import (
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}, {1, -1, 1}, {1, 1, -1},
	} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatal(o, err)
		}
	}
}

func TestNameValidation(t *testing.T) {
	l := led(t)
	for _, name := range []string{"", "A", "a b", "a/b", "é", "toolongname"} {
		if _, err := l.Apply(Batch{Ops: []Op{{Set, name, 0, 1}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatal(name, err)
		}
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "ok-nm_1", 0, 1}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Kind(99), "a", 0, 1}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestOverflowAndAbsLimit(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: math.MaxInt64})
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, math.MaxInt64}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "b", 0, math.MinInt64 + 1}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "b", -1, 0}}}); !errors.Is(err, ErrValue) {
		t.Fatal(err)
	}
	// Failed batches roll back revisions too.
	s := l.Snapshot()
	if _, err := l.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}}); err == nil {
		t.Fatal("expected error")
	}
	if !reflect.DeepEqual(l.Snapshot(), s) {
		t.Fatal("state changed after failed batch")
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	l := led(t)
	r, err := l.Apply(Batch{})
	if err != nil || r.Generation != 0 || l.Snapshot().Generation != 0 {
		t.Fatal(r, err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}}); err != nil {
		t.Fatal(err)
	}
	r, _ = l.Apply(Batch{})
	if r.Generation != 1 || l.Snapshot().Generation != 1 {
		t.Fatal(r)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	l := led(t)
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}}); err != nil {
		t.Fatal(err)
	}
	s := l.Snapshot()
	s.Accounts[0].Value = 999
	top, _ := l.Top(1)
	top[0].Value = 999
	if l.Snapshot().Accounts[0].Value != 1 {
		t.Fatal("returned slices alias internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 32, MaxNameBytes: 8, MaxAbsValue: 1000000})
	p, _ := NewPolicy(2, []string{"w"})
	c, _ := NewCoordinator(l, p)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := string(rune('a'+i%8)) + "x"
			for j := 0; j < 50; j++ {
				_, _ = c.Apply("w", Batch{Ops: []Op{{Add, name, 1, 0}}})
				_, _ = c.Apply("nope", Batch{Ops: []Op{{Add, name, 1, 0}}})
				_, _ = l.Top(4)
				_ = l.Snapshot()
				_ = c.Decisions()
			}
		}()
	}
	wg.Wait()
	// Audit sequence must be contiguous starting at 1.
	ds := c.Decisions()
	for i, d := range ds {
		if d.Sequence != uint64(i+1) {
			t.Fatal("gap in audit sequence", i, d.Sequence)
		}
	}
	// Values must equal number of committed adds per account.
	counts := map[string]int64{}
	for _, a := range l.Snapshot().Accounts {
		counts[a.Name] = a.Value
	}
	var total int64
	for _, v := range counts {
		total += v
	}
	var committed int64
	for _, d := range ds {
		if d.Committed {
			committed++
		}
	}
	if total != committed {
		t.Fatal(total, committed)
	}
}

func TestPolicyReplaceConcurrent(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 10})
	p, _ := NewPolicy(1, []string{"a"})
	c, _ := NewCoordinator(l, p)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			actor := []string{"a", "b"}[i%2]
			for j := 0; j < 100; j++ {
				_ = p.ReplaceActors([]string{actor})
				_, _ = c.Apply(actor, Batch{Ops: []Op{{Add, "k", 1, 0}}})
			}
		}()
	}
	wg.Wait()
}
