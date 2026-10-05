package metacatalog426

import (
	"reflect"
	"testing"
)

func TestMultiFileIntegration(t *testing.T) {
	s, _ := New(Options{4, 12, 8, 24})
	if err := s.ValidateBatch(Batch{Ops: []Op{{Delete, "a", []byte{}}}}); err != ErrInvalidInput {
		t.Fatalf("validation: %v", err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}}); err != nil {
		t.Fatal(err)
	}
	z := s.Stats()
	if z.Records != 1 || z.TotalValueBytes != 1 || z.NextRevision != 2 {
		t.Fatalf("stats: %+v", z)
	}
	c, err := s.Clone()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = c.Apply(Batch{Ops: []Op{{Put, "b", []byte("y")}}})
	if len(s.Snapshot().Records) != 1 || len(c.Snapshot().Records) != 2 {
		t.Fatal("clone aliases original")
	}
}

func TestPreviewParityAndIsolation(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 24})
	if _, err := s.Apply(Batch{Ops: []Op{{Kind: Put, Name: "a", Value: []byte("x")}}}); err != nil {
		t.Fatal(err)
	}
	batch := Batch{Ops: []Op{{Kind: Put, Name: "b", Value: []byte("yz")}, {Kind: Delete, Name: "a"}}}

	beforeSnapshot := s.Snapshot()
	beforeStats := s.Stats()
	candidate, err := s.Clone()
	if err != nil {
		t.Fatal(err)
	}
	wantResult, err := candidate.Apply(batch)
	if err != nil {
		t.Fatal(err)
	}

	gotResult, gotSnapshot, gotStats, err := s.Preview(batch)
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
	if !reflect.DeepEqual(s.Snapshot(), beforeSnapshot) || !reflect.DeepEqual(s.Stats(), beforeStats) {
		t.Fatal("preview mutated receiver")
	}

	zeroResult, zeroSnapshot, zeroStats, err := s.Preview(Batch{Ops: []Op{{Kind: Delete, Name: "missing"}}})
	if err != ErrNotFound {
		t.Fatalf("preview error: got=%v want=%v", err, ErrNotFound)
	}
	if !reflect.DeepEqual(zeroResult, Result{}) || !reflect.DeepEqual(zeroSnapshot, Snapshot{}) || !reflect.DeepEqual(zeroStats, Stats{}) {
		t.Fatal("failed preview returned non-zero values")
	}
	if !reflect.DeepEqual(s.Snapshot(), beforeSnapshot) || !reflect.DeepEqual(s.Stats(), beforeStats) {
		t.Fatal("failed preview mutated receiver")
	}
}
