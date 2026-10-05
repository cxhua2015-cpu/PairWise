package windowlimit

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func lim(t *testing.T) *Limiter {
	t.Helper()
	l, e := New(Options{Window: 10, Limit: 5, MaxKeys: 3, MaxKeyBytes: 12, MaxEventsPerKey: 4})
	if e != nil {
		t.Fatal(e)
	}
	return l
}
func TestValidationBeforeTime(t *testing.T) {
	l := lim(t)
	_, _ = l.Check(Batch{Now: 5})
	b := l.Snapshot()
	_, e := l.Check(Batch{Now: 4, Requests: []Request{{Key: "bad?", Units: 1}}})
	if !errors.Is(e, ErrInvalidInput) || !reflect.DeepEqual(b, l.Snapshot()) {
		t.Fatal(e)
	}
}
func TestOrderAndDenial(t *testing.T) {
	l := lim(t)
	x, e := l.Check(Batch{Now: 1, Requests: []Request{{"a", 3}, {"a", 3}, {"a", 2}}})
	if e != nil || !x.Decisions[0].Allowed || x.Decisions[1].Allowed || !x.Decisions[2].Allowed || x.Revision != 2 || x.Decisions[2].Used != 5 {
		t.Fatalf("%+v %v", x, e)
	}
}
func TestWindowBoundary(t *testing.T) {
	l := lim(t)
	_, _ = l.Check(Batch{Now: 2, Requests: []Request{{"a", 5}}})
	x, e := l.Check(Batch{Now: 12, Requests: []Request{{"a", 5}}})
	if e != nil || !x.Decisions[0].Allowed || len(l.Snapshot().Keys[0].Events) != 1 {
		t.Fatal(e)
	}
}
func TestRollbackCapacity(t *testing.T) {
	l, _ := New(Options{Window: 10, Limit: 10, MaxKeys: 1, MaxKeyBytes: 8, MaxEventsPerKey: 1})
	_, _ = l.Check(Batch{Requests: []Request{{"a", 1}}})
	b := l.Snapshot()
	_, e := l.Check(Batch{Now: 1, Requests: []Request{{"b", 1}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, l.Snapshot()) {
		t.Fatal(e)
	}
}
func TestEmptyPrunesAndGeneration(t *testing.T) {
	l := lim(t)
	_, _ = l.Check(Batch{Requests: []Request{{"a", 1}}})
	g := l.Snapshot().Generation
	x, e := l.Check(Batch{Now: 10})
	if e != nil || x.Generation != g+1 || len(l.Snapshot().Keys) != 0 {
		t.Fatal(e)
	}
}
func TestConcurrent(t *testing.T) {
	l, _ := New(Options{Window: 10, Limit: 100, MaxKeys: 64, MaxKeyBytes: 8, MaxEventsPerKey: 4})
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			k := string(rune('a' + i))
			_, _ = l.Check(Batch{Requests: []Request{{k, 1}}})
			_ = l.Snapshot()
		}()
	}
	wg.Wait()
	if len(l.Snapshot().Keys) != 24 {
		t.Fatal(len(l.Snapshot().Keys))
	}
}
