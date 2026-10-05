package usageledger

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func led(t *testing.T) *Ledger {
	t.Helper()
	l, e := New(Options{MaxAccounts: 4, MaxNameBytes: 8, MaxAbsValue: 20})
	if e != nil {
		t.Fatal(e)
	}
	return l
}
func TestOrder(t *testing.T) {
	l := led(t)
	x, e := l.Apply(Batch{Ops: []Op{{Add, "a", 3, 0}, {Add, "a", 2, 0}, {Set, "b", 0, 5}}})
	if e != nil || x.Revision != 3 || len(x.Changed) != 2 {
		t.Fatal(e, x)
	}
	top, _ := l.Top(2)
	if top[0].Name != "a" || top[1].Name != "b" {
		t.Fatal(top)
	}
}
func TestRollback(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	b := l.Snapshot()
	_, e := l.Apply(Batch{Ops: []Op{{Add, "a", 2, 0}, {Delete, "z", 0, 0}}})
	if !errors.Is(e, ErrNotFound) || !reflect.DeepEqual(b, l.Snapshot()) {
		t.Fatal(e)
	}
}
func TestFinalCapacity(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 1, MaxNameBytes: 8, MaxAbsValue: 5})
	_, _ = l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}})
	_, e := l.Apply(Batch{Ops: []Op{{Delete, "a", 0, 0}, {Set, "b", 0, -2}}})
	if e != nil || l.Snapshot().Accounts[0].Name != "b" {
		t.Fatal(e)
	}
	if _, e = l.Apply(Batch{Ops: []Op{{Add, "b", -4, 0}}}); !errors.Is(e, ErrValue) {
		t.Fatal(e)
	}
}
func TestConcurrent(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 64, MaxNameBytes: 8, MaxAbsValue: 100})
	var w sync.WaitGroup
	for i := 0; i < 20; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a' + i))
			_, _ = l.Apply(Batch{Ops: []Op{{Add, k, 1, 0}}})
			_, _ = l.Top(64)
			_ = l.Snapshot()
		}()
	}
	w.Wait()
	if len(l.Snapshot().Accounts) != 20 {
		t.Fatal(len(l.Snapshot().Accounts))
	}
}
