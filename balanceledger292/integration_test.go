package balanceledger292

import "testing"

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
