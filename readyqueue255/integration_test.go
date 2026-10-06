package readyqueue255

import "testing"

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
