package metacatalog436

import "testing"

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
