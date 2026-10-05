package windowlimit

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	valid := Options{Window: 1, Limit: 1, MaxKeys: 1, MaxKeyBytes: 1, MaxEventsPerKey: 1}
	cases := []Options{
		{},
		{Window: 0, Limit: 1, MaxKeys: 1, MaxKeyBytes: 1, MaxEventsPerKey: 1},
		{Window: -1, Limit: 1, MaxKeys: 1, MaxKeyBytes: 1, MaxEventsPerKey: 1},
		{Window: 1, Limit: 0, MaxKeys: 1, MaxKeyBytes: 1, MaxEventsPerKey: 1},
		{Window: 1, Limit: 1, MaxKeys: 0, MaxKeyBytes: 1, MaxEventsPerKey: 1},
		{Window: 1, Limit: 1, MaxKeys: 1, MaxKeyBytes: 0, MaxEventsPerKey: 1},
		{Window: 1, Limit: 1, MaxKeys: 1, MaxKeyBytes: 1, MaxEventsPerKey: 0},
		{Window: 1, Limit: 1, MaxKeys: -2, MaxKeyBytes: 1, MaxEventsPerKey: 1},
	}
	for i, o := range cases {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("case %d: %v", i, e)
		}
	}
	if _, e := New(valid); e != nil {
		t.Fatal(e)
	}
}

func TestInvalidInputs(t *testing.T) {
	l, _ := New(Options{Window: 10, Limit: 5, MaxKeys: 16, MaxKeyBytes: 16, MaxEventsPerKey: 4})
	bad := []Batch{
		{Now: -1},
		{Now: 0, Requests: []Request{{Key: "", Units: 1}}},
		{Now: 0, Requests: []Request{{Key: "a b", Units: 1}}},
		{Now: 0, Requests: []Request{{Key: "héllo", Units: 1}}},
		{Now: 0, Requests: []Request{{Key: "way_too_long_key_xx", Units: 1}}},
		{Now: 0, Requests: []Request{{Key: "a", Units: 0}}},
		{Now: 0, Requests: []Request{{Key: "a", Units: 6}}},
	}
	for i, b := range bad {
		if _, e := l.Check(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
	}
	good := []string{"a", "A", "0", ".", "_", "/", "-", "api/v1.users_2"}
	for _, k := range good {
		if _, e := l.Check(Batch{Requests: []Request{{Key: k, Units: 1}}}); e != nil {
			t.Fatalf("key %q: %v", k, e)
		}
	}
}

func TestTimeMonotonicAndRollback(t *testing.T) {
	l := lim(t)
	x, e := l.Check(Batch{Now: 7, Requests: []Request{{"a", 1}}})
	if e != nil || x.Revision != 1 {
		t.Fatal(e)
	}
	b := l.Snapshot()
	if _, e := l.Check(Batch{Now: 6}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, l.Snapshot()) {
		t.Fatal("time error mutated state")
	}
	// Equal time is allowed and empty batch keeps generation.
	x, e = l.Check(Batch{Now: 7})
	if e != nil || x.Generation != b.Generation || x.Revision != 1 {
		t.Fatalf("%+v %v", x, e)
	}
}

func TestDeniedAllocatesNoRevision(t *testing.T) {
	l := lim(t)
	x, e := l.Check(Batch{Now: 1, Requests: []Request{{"a", 5}, {"a", 1}, {"b", 1}}})
	if e != nil {
		t.Fatal(e)
	}
	if x.Decisions[1].Allowed || x.Decisions[1].Revision != 0 || x.Decisions[1].Used != 5 {
		t.Fatalf("%+v", x.Decisions[1])
	}
	if !x.Decisions[2].Allowed || x.Decisions[2].Revision != 2 {
		t.Fatalf("%+v", x.Decisions[2])
	}
	if l.Snapshot().NextRevision != 3 {
		t.Fatal(l.Snapshot().NextRevision)
	}
}

func TestWindowLeftBoundaryExcluded(t *testing.T) {
	l := lim(t)
	_, _ = l.Check(Batch{Now: 5, Requests: []Request{{"a", 5}}})
	// At == Now-Window is pruned: event at 5, Now=15 -> cutoff 5.
	x, e := l.Check(Batch{Now: 15, Requests: []Request{{"a", 5}}})
	if e != nil || !x.Decisions[0].Allowed || len(l.Snapshot().Keys[0].Events) != 1 {
		t.Fatalf("%+v %v", x, e)
	}
	// Event strictly inside the window still counts.
	l2 := lim(t)
	_, _ = l2.Check(Batch{Now: 5, Requests: []Request{{"a", 5}}})
	x, e = l2.Check(Batch{Now: 14, Requests: []Request{{"a", 1}}})
	if e != nil || x.Decisions[0].Allowed {
		t.Fatalf("%+v %v", x, e)
	}
}

func TestCapacityEventsPerKeyRollback(t *testing.T) {
	l, _ := New(Options{Window: 100, Limit: 10, MaxKeys: 2, MaxKeyBytes: 8, MaxEventsPerKey: 2})
	_, _ = l.Check(Batch{Requests: []Request{{"a", 1}, {"a", 1}}})
	b := l.Snapshot()
	// Third event for "a" exceeds MaxEventsPerKey; whole batch rolls back,
	// including the otherwise-allowed request for "b".
	_, e := l.Check(Batch{Now: 1, Requests: []Request{{"b", 1}, {"a", 1}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, l.Snapshot()) {
		t.Fatal("capacity failure mutated state")
	}
}

func TestCapacityPruneCanAvoidFailure(t *testing.T) {
	l, _ := New(Options{Window: 10, Limit: 10, MaxKeys: 1, MaxKeyBytes: 8, MaxEventsPerKey: 1})
	_, _ = l.Check(Batch{Now: 0, Requests: []Request{{"a", 1}}})
	// Pruning removes "a" before capacity check, so "b" succeeds.
	x, e := l.Check(Batch{Now: 10, Requests: []Request{{"b", 1}}})
	if e != nil || !x.Decisions[0].Allowed {
		t.Fatalf("%+v %v", x, e)
	}
	s := l.Snapshot()
	if len(s.Keys) != 1 || s.Keys[0].Key != "b" {
		t.Fatalf("%+v", s.Keys)
	}
}

func TestSnapshotOrderingAndIsolation(t *testing.T) {
	l, _ := New(Options{Window: 100, Limit: 100, MaxKeys: 8, MaxKeyBytes: 8, MaxEventsPerKey: 8})
	_, _ = l.Check(Batch{Now: 1, Requests: []Request{{"b", 1}, {"a", 1}}})
	_, _ = l.Check(Batch{Now: 3, Requests: []Request{{"a", 1}}})
	_, _ = l.Check(Batch{Now: 2, Requests: []Request{{"a", 1}}}) // time error ignored? no: 2 < 3
	s := l.Snapshot()
	if len(s.Keys) != 2 || s.Keys[0].Key != "a" || s.Keys[1].Key != "b" {
		t.Fatalf("%+v", s.Keys)
	}
	evs := s.Keys[0].Events
	if len(evs) != 2 || evs[0].At != 1 || evs[1].At != 3 {
		t.Fatalf("%+v", evs)
	}
	// Mutating the snapshot must not affect the limiter.
	s.Keys[0].Events[0].Units = 999
	s.Keys[0].Key = "zzz"
	again := l.Snapshot()
	if again.Keys[0].Key != "a" || again.Keys[0].Events[0].Units != 1 {
		t.Fatal("snapshot shares memory with limiter")
	}
}

func TestResultRevisionZeroOnEmptyFirstBatch(t *testing.T) {
	l := lim(t)
	x, e := l.Check(Batch{Now: 3})
	if e != nil || x.Revision != 0 || x.Generation != 0 || len(x.Decisions) != 0 {
		t.Fatalf("%+v %v", x, e)
	}
	if s := l.Snapshot(); s.Now != 3 || s.NextRevision != 1 {
		t.Fatalf("%+v", s)
	}
}

func TestConcurrentCheckAndSnapshot(t *testing.T) {
	l, _ := New(Options{Window: 50, Limit: 4, MaxKeys: 128, MaxKeyBytes: 16, MaxEventsPerKey: 64})
	var wg sync.WaitGroup
	allowed := make(chan bool, 4096)
	for g := 0; g < 8; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				key := fmt.Sprintf("key-%d", (g*100+i)%32)
				x, e := l.Check(Batch{Now: int64(i), Requests: []Request{{Key: key, Units: 1}}})
				if e == nil {
					for _, d := range x.Decisions {
						allowed <- d.Allowed
					}
				}
				_ = l.Snapshot()
			}
		}()
	}
	wg.Wait()
	close(allowed)
	var total int
	for a := range allowed {
		if a {
			total++
		}
	}
	s := l.Snapshot()
	var live uint64
	for _, k := range s.Keys {
		for _, e := range k.Events {
			live += e.Units
		}
		if len(k.Events) > 64 {
			t.Fatal("event cap violated")
		}
	}
	if len(s.Keys) > 128 {
		t.Fatal("key cap violated")
	}
	_ = total
	_ = live
}
