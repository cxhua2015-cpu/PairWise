package prefixclaim

import (
	"errors"
	"testing"
)

func TestCapacityRollbackKeepsRevision(t *testing.T) {
	r, _ := New(Options{MaxClaims: 1, MaxPathBytes: 16, MaxOwnerBytes: 8})
	if _, e := r.Apply(Batch{Ops: []Op{claim("/a", "o"), claim("/b", "o")}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := r.Snapshot()
	if s.Generation != 0 || s.NextRevision != 1 || len(s.Entries) != 0 {
		t.Fatalf("s=%+v", s)
	}
	x, e := r.Apply(Batch{Ops: []Op{claim("/a", "o")}})
	if e != nil || x.Revision != 1 || x.Generation != 1 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
}

func TestChangedSurvivingSorted(t *testing.T) {
	r := reg(t)
	x, e := r.Apply(Batch{Ops: []Op{claim("/b", "o"), claim("/a", "o")}})
	if e != nil || len(x.Changed) != 2 || x.Changed[0].Path != "/a" || x.Changed[1].Path != "/b" {
		t.Fatalf("x=%+v e=%v", x, e)
	}
	x, e = r.Apply(Batch{Ops: []Op{release("/a", "o"), claim("/a", "o2")}})
	if e != nil || len(x.Changed) != 1 || x.Changed[0].Owner != "o2" {
		t.Fatalf("x=%+v e=%v", x, e)
	}
	if x.Changed[0].Owner != "o2" {
		t.Fatalf("x=%+v", x)
	}
	x, e = r.Apply(Batch{Ops: []Op{release("/a", "o2")}})
	if e != nil || len(x.Changed) != 0 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
}

func TestDescendantsValidationAndIsolation(t *testing.T) {
	r := reg(t)
	_, _ = r.Apply(Batch{Ops: []Op{claim("/a/b", "o"), claim("/a/c", "o")}})
	if _, e := r.Descendants("/a", "", 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := r.Descendants("/a", "", 1001); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := r.Descendants("/a", "bad", 1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := r.Descendants("a", "", 1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	x, e := r.Descendants("/", "", 10)
	if e != nil || len(x) != 2 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
	x[0].Owner = "mutated"
	s := r.Snapshot()
	if s.Entries[0].Owner != "o" {
		t.Fatalf("s=%+v", s)
	}
	s.Entries[0].Owner = "mutated"
	if r.Snapshot().Entries[0].Owner != "o" {
		t.Fatal("snapshot not isolated")
	}
}

func TestLookupValidationAndMiss(t *testing.T) {
	r := reg(t)
	if _, _, e := r.Lookup("//x"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := r.Lookup("/nope"); e != nil || ok {
		t.Fatal("expected miss")
	}
	_, _ = r.Apply(Batch{Ops: []Op{claim("/", "root")}})
	e, ok, err := r.Lookup("/any/depth")
	if err != nil || !ok || e.Path != "/" {
		t.Fatalf("e=%+v ok=%v err=%v", e, ok, err)
	}
}

func TestReleaseNotFoundAndEmptyBatch(t *testing.T) {
	r := reg(t)
	if _, e := r.Apply(Batch{Ops: []Op{release("/a", "o")}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	x, e := r.Apply(Batch{})
	if e != nil || x.Generation != 0 || x.Revision != 0 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
	if _, e := r.Apply(Batch{Ops: []Op{{Kind: 0, Path: "/a", Owner: "o"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}
