package ledger

import (
	"errors"
	"testing"
)

func TestEmptyBatchNoGeneration(t *testing.T) {
	l := led(t)
	x, e := l.Apply(Batch{})
	if e != nil || x.Generation != 0 || x.Revision != 0 || len(x.Changed) != 0 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
	if s := l.Snapshot(); s.Generation != 0 || s.NextRevision != 1 {
		t.Fatalf("s=%+v", s)
	}
}

func TestGetValidation(t *testing.T) {
	l := led(t)
	if _, _, e := l.Get("bad name"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := l.Get("nope"); e != nil || ok {
		t.Fatalf("ok=%v e=%v", ok, e)
	}
}

func TestCloseReopenSameBatch(t *testing.T) {
	l := led(t)
	x, e := l.Apply(Batch{Ops: []Op{
		op(Open, "a", "", 5),
		op(Debit, "a", "", 5),
		op(Close, "a", "", 0),
		op(Open, "a", "", 7),
	}})
	if e != nil || x.Revision != 4 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
	a, ok, _ := l.Get("a")
	if !ok || a.Balance != 7 || a.Revision != 4 {
		t.Fatalf("a=%+v ok=%v", a, ok)
	}
	if len(x.Changed) != 1 || x.Changed[0].Name != "a" {
		t.Fatalf("changed=%+v", x.Changed)
	}
}

func TestSnapshotSortedAndChangedSorted(t *testing.T) {
	l := led(t)
	x, e := l.Apply(Batch{Ops: []Op{
		op(Open, "z", "", 1),
		op(Open, "m", "", 2),
		op(Open, "a", "", 3),
	}})
	if e != nil {
		t.Fatal(e)
	}
	names := []string{"a", "m", "z"}
	for i, n := range names {
		if x.Changed[i].Name != n || l.Snapshot().Accounts[i].Name != n {
			t.Fatalf("changed=%+v snap=%+v", x.Changed, l.Snapshot().Accounts)
		}
	}
}

func TestTemporaryOverdraftOk(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{op(Open, "a", "", 1)}})
	_, e := l.Apply(Batch{Ops: []Op{
		op(Debit, "a", "", 2),
		op(Credit, "a", "", 5),
	}})
	if !errors.Is(e, ErrFunds) {
		t.Fatal(e)
	}
	x, e := l.Apply(Batch{Ops: []Op{
		op(Credit, "a", "", 5),
		op(Debit, "a", "", 6),
	}})
	if e != nil || x.Revision != 3 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
	a, _, _ := l.Get("a")
	if a.Balance != 0 {
		t.Fatalf("a=%+v", a)
	}
}
