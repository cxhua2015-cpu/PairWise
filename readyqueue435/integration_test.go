package readyqueue435

import (
	"reflect"
	"testing"
)

func TestMultiFileIntegration(t *testing.T) {
	q, _ := New(Options{4, 12})
	if err := q.ValidateBatch(Batch{Ops: []Op{{Cancel, "a", 1, 0}}}); err != ErrInvalidInput {
		t.Fatalf("validation: %v", err)
	}
	if _, err := q.Apply(Batch{Now: 1, Ops: []Op{{Enqueue, "a", 2, 2}}}); err != nil {
		t.Fatal(err)
	}
	z := q.Stats()
	if z.Items != 1 || z.Now != 1 || z.NextRevision != 2 {
		t.Fatalf("stats: %+v", z)
	}
	c, err := q.Clone()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = c.Pop(2, 1)
	if len(q.Snapshot().Items) != 1 || len(c.Snapshot().Items) != 0 {
		t.Fatal("clone aliases original")
	}
}

func TestPreviewParityAndIsolation(t *testing.T) {
	q, _ := New(Options{MaxItems: 4, MaxIDBytes: 12})
	if _, err := q.Apply(Batch{Now: 1, Ops: []Op{{Kind: Enqueue, ID: "a", Priority: 1, ReadyAt: 2}}}); err != nil {
		t.Fatal(err)
	}
	batch := Batch{Now: 2, Ops: []Op{{Kind: Cancel, ID: "a"}, {Kind: Enqueue, ID: "b", Priority: 3, ReadyAt: 3}}}

	beforeSnapshot := q.Snapshot()
	beforeStats := q.Stats()
	candidate, err := q.Clone()
	if err != nil {
		t.Fatal(err)
	}
	wantResult, err := candidate.Apply(batch)
	if err != nil {
		t.Fatal(err)
	}

	gotResult, gotSnapshot, gotStats, err := q.Preview(batch)
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
	if !reflect.DeepEqual(q.Snapshot(), beforeSnapshot) || !reflect.DeepEqual(q.Stats(), beforeStats) {
		t.Fatal("preview mutated receiver")
	}

	zeroResult, zeroSnapshot, zeroStats, err := q.Preview(Batch{Now: 0})
	if err != ErrTime {
		t.Fatalf("preview error: got=%v want=%v", err, ErrTime)
	}
	if !reflect.DeepEqual(zeroResult, Result{}) || !reflect.DeepEqual(zeroSnapshot, Snapshot{}) || !reflect.DeepEqual(zeroStats, Stats{}) {
		t.Fatal("failed preview returned non-zero values")
	}
	if !reflect.DeepEqual(q.Snapshot(), beforeSnapshot) || !reflect.DeepEqual(q.Stats(), beforeStats) {
		t.Fatal("failed preview mutated receiver")
	}
}
