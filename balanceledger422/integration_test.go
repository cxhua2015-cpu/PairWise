package balanceledger422

import (
	"reflect"
	"testing"
)

func TestMultiFileIntegration(t *testing.T) {
	l, _ := New(Options{4, 12, 100})
	if err := l.ValidateBatch(Batch{Ops: []Op{{Add, "a", 0, 0}}}); err != ErrInvalidInput {
		t.Fatalf("validation: %v", err)
	}
	if _, err := l.Apply(Batch{Ops: []Op{{Set, "a", 0, 1}}}); err != nil {
		t.Fatal(err)
	}
	z := l.Stats()
	if z.Accounts != 1 || z.NextRevision != 2 {
		t.Fatalf("stats: %+v", z)
	}
	c, err := l.Clone()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = c.Apply(Batch{Ops: []Op{{Add, "a", 1, 0}}})
	a := l.Snapshot().Accounts[0]
	b := c.Snapshot().Accounts[0]
	if a.Value != 1 || b.Value != 2 {
		t.Fatal("clone aliases original")
	}
}

func TestPreviewParityAndIsolation(t *testing.T) {
	l, _ := New(Options{MaxAccounts: 4, MaxNameBytes: 12, MaxAbsValue: 100})
	if _, err := l.Apply(Batch{Ops: []Op{{Kind: Set, Name: "a", Value: 1}}}); err != nil {
		t.Fatal(err)
	}
	batch := Batch{Ops: []Op{{Kind: Add, Name: "a", Delta: 2}, {Kind: Set, Name: "b", Value: 4}}}

	beforeSnapshot := l.Snapshot()
	beforeStats := l.Stats()
	candidate, err := l.Clone()
	if err != nil {
		t.Fatal(err)
	}
	wantResult, err := candidate.Apply(batch)
	if err != nil {
		t.Fatal(err)
	}

	gotResult, gotSnapshot, gotStats, err := l.Preview(batch)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotResult, wantResult) {
		t.Fatalf("result mismatch: got=%+v want=%+v", gotResult, wantResult)
	}
	if !reflect.DeepEqual(gotSnapshot, candidate.Snapshot()) {
		t.Fatalf("snapshot mismatch: got=%+v want=%+v", gotSnapshot, candidate.Snapshot())
	}
	if !reflect.DeepEqual(gotStats, candidate.Stats()) {
		t.Fatalf("stats mismatch: got=%+v want=%+v", gotStats, candidate.Stats())
	}
	if !reflect.DeepEqual(l.Snapshot(), beforeSnapshot) || !reflect.DeepEqual(l.Stats(), beforeStats) {
		t.Fatal("preview mutated receiver")
	}

	zeroResult, zeroSnapshot, zeroStats, err := l.Preview(Batch{Ops: []Op{{Kind: Delete, Name: "missing"}}})
	if err != ErrNotFound {
		t.Fatalf("preview error: got=%v want=%v", err, ErrNotFound)
	}
	if !reflect.DeepEqual(zeroResult, Result{}) || !reflect.DeepEqual(zeroSnapshot, Snapshot{}) || !reflect.DeepEqual(zeroStats, Stats{}) {
		t.Fatal("failed preview returned non-zero values")
	}
	if !reflect.DeepEqual(l.Snapshot(), beforeSnapshot) || !reflect.DeepEqual(l.Stats(), beforeStats) {
		t.Fatal("failed preview mutated receiver")
	}
}
