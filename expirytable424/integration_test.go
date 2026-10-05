package expirytable424

import (
	"reflect"
	"testing"
)

func TestMultiFileIntegration(t *testing.T) {
	x, _ := New(Options{4, 12})
	if err := x.ValidateBatch(Batch{Now: 2, Ops: []Op{{Put, "a", 2}}}); err != ErrInvalidInput {
		t.Fatalf("validation: %v", err)
	}
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}}}); err != nil {
		t.Fatal(err)
	}
	z := x.Stats()
	if z.Entries != 1 || z.Now != 1 || z.NextRevision != 2 {
		t.Fatalf("stats: %+v", z)
	}
	c, err := x.Clone()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = c.Expire(5)
	if len(x.Snapshot().Entries) != 1 || len(c.Snapshot().Entries) != 0 {
		t.Fatal("clone aliases original")
	}
}

func TestPreviewParityAndIsolation(t *testing.T) {
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 12})
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Kind: Put, Key: "a", ExpiresAt: 5}}}); err != nil {
		t.Fatal(err)
	}
	batch := Batch{Now: 2, Ops: []Op{{Kind: Touch, Key: "a", ExpiresAt: 7}, {Kind: Put, Key: "b", ExpiresAt: 8}}}

	beforeSnapshot := x.Snapshot()
	beforeStats := x.Stats()
	candidate, err := x.Clone()
	if err != nil {
		t.Fatal(err)
	}
	wantResult, err := candidate.Apply(batch)
	if err != nil {
		t.Fatal(err)
	}

	gotResult, gotSnapshot, gotStats, err := x.Preview(batch)
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
	if !reflect.DeepEqual(x.Snapshot(), beforeSnapshot) || !reflect.DeepEqual(x.Stats(), beforeStats) {
		t.Fatal("preview mutated receiver")
	}

	zeroResult, zeroSnapshot, zeroStats, err := x.Preview(Batch{Now: 0})
	if err != ErrTime {
		t.Fatalf("preview error: got=%v want=%v", err, ErrTime)
	}
	if !reflect.DeepEqual(zeroResult, Result{}) || !reflect.DeepEqual(zeroSnapshot, Snapshot{}) || !reflect.DeepEqual(zeroStats, Stats{}) {
		t.Fatal("failed preview returned non-zero values")
	}
	if !reflect.DeepEqual(x.Snapshot(), beforeSnapshot) || !reflect.DeepEqual(x.Stats(), beforeStats) {
		t.Fatal("failed preview mutated receiver")
	}
}
