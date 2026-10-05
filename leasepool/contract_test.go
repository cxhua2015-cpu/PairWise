package leasepool

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func pool(t *testing.T) *Pool {
	t.Helper()
	p, e := New(Options{MaxResources: 3, MaxNameBytes: 12, MaxOwnerBytes: 12})
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func TestValidationBeforeTime(t *testing.T) {
	p := pool(t)
	_, _ = p.Apply(Batch{Now: 5})
	before := p.Snapshot()
	_, e := p.Apply(Batch{Now: 4, Ops: []Op{{Kind: Add, Resource: "bad?"}}})
	if !errors.Is(e, ErrInvalidInput) || !reflect.DeepEqual(before, p.Snapshot()) {
		t.Fatalf("%v", e)
	}
}
func TestBatchOrderRevisionAndRollback(t *testing.T) {
	p := pool(t)
	x, e := p.Apply(Batch{Now: 1, Ops: []Op{{Kind: Add, Resource: "cpu"}, {Kind: Acquire, Resource: "cpu", Owner: "a", ExpiresAt: 5}, {Kind: Renew, Resource: "cpu", Owner: "a", ExpiresAt: 7}}})
	if e != nil || x.Revision != 3 || len(x.Changed) != 1 || x.Changed[0].Revision != 3 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
	before := p.Snapshot()
	_, e = p.Apply(Batch{Now: 2, Ops: []Op{{Kind: Release, Resource: "cpu", Owner: "bad"}, {Kind: Remove, Resource: "cpu"}}})
	if !errors.Is(e, ErrOwner) || !reflect.DeepEqual(before, p.Snapshot()) {
		t.Fatal(e)
	}
}
func TestExpiredAcquireAndExpireBoundary(t *testing.T) {
	p := pool(t)
	_, _ = p.Apply(Batch{Ops: []Op{{Kind: Add, Resource: "a"}, {Kind: Add, Resource: "b"}, {Kind: Acquire, Resource: "a", Owner: "x", ExpiresAt: 3}, {Kind: Acquire, Resource: "b", Owner: "y", ExpiresAt: 4}}})
	x, e := p.Apply(Batch{Now: 3, Ops: []Op{{Kind: Acquire, Resource: "a", Owner: "z", ExpiresAt: 8}}})
	if e != nil || x.Changed[0].Owner != "z" {
		t.Fatal(e)
	}
	gone, e := p.Expire(4)
	if e != nil || len(gone) != 1 || gone[0].Resource != "b" {
		t.Fatalf("%+v %v", gone, e)
	}
}
func TestFinalCapacityAndRemoveRule(t *testing.T) {
	p, _ := New(Options{MaxResources: 1, MaxNameBytes: 8, MaxOwnerBytes: 8})
	_, _ = p.Apply(Batch{Ops: []Op{{Kind: Add, Resource: "a"}}})
	_, e := p.Apply(Batch{Ops: []Op{{Kind: Remove, Resource: "a"}, {Kind: Add, Resource: "b"}}})
	if e != nil || !reflect.DeepEqual(p.Snapshot().Resources, []string{"b"}) {
		t.Fatal(e)
	}
	_, _ = p.Apply(Batch{Ops: []Op{{Kind: Acquire, Resource: "b", Owner: "o", ExpiresAt: 2}}})
	if _, e = p.Apply(Batch{Now: 2, Ops: []Op{{Kind: Remove, Resource: "b"}}}); !errors.Is(e, ErrBusy) {
		t.Fatal(e)
	}
}
func TestEmptyAndTime(t *testing.T) {
	p := pool(t)
	g := p.Snapshot().Generation
	x, e := p.Apply(Batch{Now: 4})
	if e != nil || x.Generation != g || p.Snapshot().Now != 4 {
		t.Fatal(e)
	}
	if _, e = p.Expire(3); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
}
func TestConcurrent(t *testing.T) {
	p, _ := New(Options{MaxResources: 64, MaxNameBytes: 16, MaxOwnerBytes: 8})
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := string(rune('a' + i))
			_, _ = p.Apply(Batch{Ops: []Op{{Kind: Add, Resource: r}}})
			_ = p.Snapshot()
		}()
	}
	wg.Wait()
	if len(p.Snapshot().Resources) != 24 {
		t.Fatal(len(p.Snapshot().Resources))
	}
}
