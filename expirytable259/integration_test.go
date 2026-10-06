package expirytable259

import "testing"

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
