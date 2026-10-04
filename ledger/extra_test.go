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

func TestChangedSortedDedup(t *testing.T) {
	l := led(t)
	x, e := l.Apply(Batch{Ops: []Op{
		op(Open, "b", "", 5), op(Open, "a", "", 0),
		op(Transfer, "b", "a", 2), op(Credit, "b", "", 1),
	}})
	if e != nil {
		t.Fatal(e)
	}
	if len(x.Changed) != 2 || x.Changed[0].Name != "a" || x.Changed[1].Name != "b" {
		t.Fatalf("changed=%+v", x.Changed)
	}
	if x.Changed[0].Balance != 2 || x.Changed[1].Balance != 4 {
		t.Fatalf("changed=%+v", x.Changed)
	}
}

func TestGetValidatesName(t *testing.T) {
	l := led(t)
	if _, _, e := l.Get("bad?"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := l.Get("missing"); e != nil || ok {
		t.Fatal(ok, e)
	}
}

func TestStructuralValidationErrors(t *testing.T) {
	l := led(t)
	cases := []Op{
		{Kind: 0, Account: "a"},
		op(Open, "", "", 0),
		op(Open, "a", "", -1),
		op(Credit, "a", "x", 1),
		op(Debit, "a", "", 0),
		op(Transfer, "a", "a", 1),
		op(Transfer, "a", "", 1),
		op(Close, "a", "", 1),
		op(Close, "a", "x", 0),
	}
	for _, c := range cases {
		if _, e := l.Apply(Batch{Ops: []Op{c}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op=%+v e=%v", c, e)
		}
	}
}

func TestCloseReopenSameBatch(t *testing.T) {
	l := led(t)
	x, e := l.Apply(Batch{Ops: []Op{
		op(Open, "a", "", 0), op(Close, "a", "", 0), op(Open, "a", "", 7),
	}})
	if e != nil || x.Revision != 3 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
	a, ok, _ := l.Get("a")
	if !ok || a.Balance != 7 || a.Revision != 3 {
		t.Fatalf("a=%+v", a)
	}
	if len(x.Changed) != 1 || x.Changed[0].Name != "a" {
		t.Fatalf("changed=%+v", x.Changed)
	}
}

func TestTemporaryOverdraftThenCompliant(t *testing.T) {
	l := led(t)
	_, _ = l.Apply(Batch{Ops: []Op{op(Open, "a", "", 5)}})
	_, e := l.Apply(Batch{Ops: []Op{
		op(Debit, "a", "", 9), op(Credit, "a", "", 4),
	}})
	if !errors.Is(e, ErrFunds) {
		t.Fatal(e)
	}
	a, _, _ := l.Get("a")
	if a.Balance != 5 || a.Revision != 1 {
		t.Fatalf("a=%+v", a)
	}
	// 转账后继续扣款
	x, e := l.Apply(Batch{Ops: []Op{
		op(Open, "b", "", 0), op(Transfer, "a", "b", 3), op(Debit, "b", "", 2),
	}})
	if e != nil || x.Revision != 4 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
	b, _, _ := l.Get("b")
	if b.Balance != 1 || b.Revision != 4 {
		t.Fatalf("b=%+v", b)
	}
}
