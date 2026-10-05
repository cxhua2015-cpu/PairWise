package windowlimit

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func TestNewInvalidOptions(t *testing.T) {
	valid := Options{Window: 1, Limit: 1, MaxKeys: 1, MaxKeyBytes: 1, MaxEventsPerKey: 1}
	bads := []Options{}
	for _, mutate := range []func(*Options){
		func(o *Options) { o.Window = 0 },
		func(o *Options) { o.Window = -1 },
		func(o *Options) { o.Limit = 0 },
		func(o *Options) { o.MaxKeys = 0 },
		func(o *Options) { o.MaxKeyBytes = 0 },
		func(o *Options) { o.MaxEventsPerKey = 0 },
	} {
		o := valid
		mutate(&o)
		bads = append(bads, o)
	}
	for _, o := range bads {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
	if _, e := New(valid); e != nil {
		t.Fatal(e)
	}
}

func TestKeyValidation(t *testing.T) {
	l, _ := New(Options{Window: 10, Limit: 5, MaxKeys: 4, MaxKeyBytes: 3, MaxEventsPerKey: 4})
	for _, k := range []string{"", "a b", "a?", "é", "abcd", "a\tb"} {
		if _, e := l.Check(Batch{Requests: []Request{{k, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	for _, k := range []string{"a", "A-1", "a.b", "abc"} {
		if _, e := l.Check(Batch{Requests: []Request{{k, 1}}}); e != nil {
			t.Fatalf("key %q: %v", k, e)
		}
	}
}

func TestUnitsValidation(t *testing.T) {
	l := lim(t)
	for _, u := range []uint64{0, 6, 100} {
		if _, e := l.Check(Batch{Requests: []Request{{"a", u}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("units %d: %v", u, e)
		}
	}
	if _, e := l.Check(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestTimeMonotonic(t *testing.T) {
	l := lim(t)
	if _, e := l.Check(Batch{Now: 5}); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Check(Batch{Now: 4}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := l.Check(Batch{Now: 5}); e != nil {
		t.Fatal(e)
	}
}

func TestBoundaryExclusive(t *testing.T) {
	l := lim(t)
	_, _ = l.Check(Batch{Now: 2, Requests: []Request{{"a", 5}}})
	// At Now=12 the event at At=2 is exactly on the excluded boundary (2 <= 12-10).
	x, _ := l.Check(Batch{Now: 12, Requests: []Request{{"a", 5}}})
	if !x.Decisions[0].Allowed {
		t.Fatal("boundary event should be pruned")
	}
	// At Now=11 the event at At=2 is still live (2 > 11-10).
	l2 := lim(t)
	_, _ = l2.Check(Batch{Now: 2, Requests: []Request{{"a", 5}}})
	y, _ := l2.Check(Batch{Now: 11, Requests: []Request{{"a", 1}}})
	if y.Decisions[0].Allowed {
		t.Fatal("event one tick inside window should still count")
	}
}

func TestRollbackKeepsTimeAndRevision(t *testing.T) {
	l, _ := New(Options{Window: 10, Limit: 10, MaxKeys: 1, MaxKeyBytes: 8, MaxEventsPerKey: 4})
	r1, _ := l.Check(Batch{Now: 1, Requests: []Request{{"a", 1}}})
	b := l.Snapshot()
	// Capacity failure at a later time must not advance time, generation or revision.
	if _, e := l.Check(Batch{Now: 5, Requests: []Request{{"b", 1}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := l.Snapshot(); !reflect.DeepEqual(b, got) {
		t.Fatalf("%+v vs %+v", b, got)
	}
	r2, _ := l.Check(Batch{Now: 5, Requests: []Request{{"a", 1}}})
	if r2.Revision != r1.Revision+1 || r2.Generation != r1.Generation+1 {
		t.Fatalf("r1=%+v r2=%+v", r1, r2)
	}
}

func TestCapacityEventsPerKey(t *testing.T) {
	l, _ := New(Options{Window: 100, Limit: 100, MaxKeys: 4, MaxKeyBytes: 8, MaxEventsPerKey: 2})
	if _, e := l.Check(Batch{Requests: []Request{{"a", 1}, {"a", 1}, {"a", 1}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(l.Snapshot().Keys); n != 0 {
		t.Fatal("rollback expected")
	}
}

func TestEmptyBatchNoChangeNoGeneration(t *testing.T) {
	l := lim(t)
	x, e := l.Check(Batch{Now: 3})
	if e != nil || x.Generation != 0 || x.Revision != 0 {
		t.Fatalf("%+v %v", x, e)
	}
	if l.Snapshot().Now != 3 {
		t.Fatal("empty batch should advance time")
	}
}

func TestDecisionFields(t *testing.T) {
	l := lim(t)
	x, _ := l.Check(Batch{Now: 1, Requests: []Request{{"a", 2}, {"a", 4}, {"b", 5}}})
	d := x.Decisions
	if !d[0].Allowed || d[0].Used != 2 || d[0].Remaining != 3 || d[0].Revision != 1 {
		t.Fatalf("%+v", d[0])
	}
	if d[1].Allowed || d[1].Used != 2 || d[1].Remaining != 3 || d[1].Revision != 0 {
		t.Fatalf("%+v", d[1])
	}
	if !d[2].Allowed || d[2].Used != 5 || d[2].Remaining != 0 || d[2].Revision != 2 {
		t.Fatalf("%+v", d[2])
	}
}

func TestSnapshotIsolationAndOrder(t *testing.T) {
	l := lim(t)
	_, _ = l.Check(Batch{Now: 1, Requests: []Request{{"b", 1}, {"a", 1}, {"a", 1}}})
	s := l.Snapshot()
	if len(s.Keys) != 2 || s.Keys[0].Key != "a" || s.Keys[1].Key != "b" {
		t.Fatalf("%+v", s.Keys)
	}
	evs := s.Keys[0].Events
	if len(evs) != 2 || evs[0].Revision >= evs[1].Revision {
		t.Fatalf("%+v", evs)
	}
	// Mutating the snapshot must not affect the limiter.
	s.Keys[0].Events[0].Units = 99
	s.Keys[0].Key = "zzz"
	s2 := l.Snapshot()
	if s2.Keys[0].Key != "a" || s2.Keys[0].Events[0].Units != 1 {
		t.Fatal("snapshot not isolated")
	}
}

func TestConcurrentContention(t *testing.T) {
	l, _ := New(Options{Window: 10, Limit: 8, MaxKeys: 4, MaxKeyBytes: 8, MaxEventsPerKey: 64})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			k := string(rune('a' + i%4))
			for j := 0; j < 50; j++ {
				r, err := l.Check(Batch{Now: int64(j), Requests: []Request{{k, 1}}})
				if err == nil && len(r.Decisions) != 1 {
					t.Error("bad decisions")
				}
				_ = l.Snapshot()
			}
		}()
	}
	wg.Wait()
	s := l.Snapshot()
	var total uint64
	for _, ks := range s.Keys {
		if len(ks.Events) > 64 {
			t.Fatal("event cap violated")
		}
		var sum uint64
		for _, e := range ks.Events {
			sum += e.Units
		}
		if sum > 8 {
			t.Fatal("limit violated")
		}
		total += sum
	}
	if total == 0 {
		t.Fatal("no events recorded")
	}
}
