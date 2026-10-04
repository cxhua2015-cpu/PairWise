package prefixclaim

import (
	"errors"
	"testing"
)

func TestRevisionNotConsumedOnFailure(t *testing.T) {
	r := reg(t)
	_, _ = r.Apply(Batch{Ops: []Op{claim("/a", "o")}})
	if _, e := r.Apply(Batch{Ops: []Op{claim("/b", "o"), claim("/b/c", "o")}}); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	s := r.Snapshot()
	if s.NextRevision != 2 || s.Generation != 1 || len(s.Entries) != 1 {
		t.Fatalf("s=%+v", s)
	}
	x, e := r.Apply(Batch{Ops: []Op{claim("/b", "o")}})
	if e != nil || x.Revision != 2 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
}

func TestReleaseNotFoundAndEmptyBatch(t *testing.T) {
	r := reg(t)
	if _, e := r.Apply(Batch{Ops: []Op{release("/nope", "o")}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	x, e := r.Apply(Batch{})
	if e != nil || x.Generation != 0 || len(x.Changed) != 0 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
}

func TestChangedSurvivingSorted(t *testing.T) {
	r := reg(t)
	x, e := r.Apply(Batch{Ops: []Op{claim("/b", "o"), claim("/a", "o"), claim("/c", "o"), release("/c", "o")}})
	if e != nil || len(x.Changed) != 2 || x.Changed[0].Path != "/a" || x.Changed[1].Path != "/b" {
		t.Fatalf("x=%+v e=%v", x, e)
	}
}

func TestReturnedSlicesIsolated(t *testing.T) {
	r := reg(t)
	_, _ = r.Apply(Batch{Ops: []Op{claim("/a", "o")}})
	s := r.Snapshot()
	s.Entries[0].Owner = "mutated"
	d, _ := r.Descendants("/", "", 10)
	d[0].Owner = "mutated"
	e, ok, _ := r.Lookup("/a")
	if !ok || e.Owner != "o" {
		t.Fatalf("e=%+v ok=%v", e, ok)
	}
}

func TestDescendantsValidation(t *testing.T) {
	r := reg(t)
	if _, e := r.Descendants("/bad//x", "", 1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := r.Descendants("/a", "not-a-path", 1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := r.Descendants("/a", "", 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := r.Descendants("/a", "", 1001); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, _, e := r.Lookup("relative"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestInvalidOps(t *testing.T) {
	r := reg(t)
	bad := []Op{
		{Kind: 0, Path: "/a", Owner: "o"},
		claim("a", "o"),
		claim("/a/", "o"),
		claim("/a//b", "o"),
		claim("/a b", "o"),
		claim("/a", ""),
		claim("/a", "bad owner"),
	}
	for _, op := range bad {
		if _, e := r.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op=%+v e=%v", op, e)
		}
	}
	if _, e := r.Apply(Batch{Ops: []Op{claim("/", "root")}}); e != nil {
		t.Fatal(e)
	}
}
