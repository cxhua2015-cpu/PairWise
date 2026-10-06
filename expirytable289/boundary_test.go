package expirytable289

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func mustTable(t *testing.T, o Options) *Table {
	t.Helper()
	x, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 4}, {4, -1}} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestKeyAlphabetAndLength(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 8, MaxKeyBytes: 4})
	for _, k := range []string{"", "A", "a b", "a.b", "abcde", "é"} {
		if err := x.ValidateBatch(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, err)
		}
	}
	if err := x.ValidateBatch(Batch{Ops: []Op{{Put, "a-z0", 1}}}); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownKindAndExpiredPut(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 4, MaxKeyBytes: 8})
	if err := x.ValidateBatch(Batch{Ops: []Op{{Kind(0), "a", 1}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if err := x.ValidateBatch(Batch{Ops: []Op{{Kind(9), "a", 1}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	// Put/Touch with ExpiresAt <= Now is structurally invalid.
	if err := x.ValidateBatch(Batch{Now: 3, Ops: []Op{{Put, "a", 3}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if err := x.ValidateBatch(Batch{Now: 3, Ops: []Op{{Touch, "a", 2}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestValidateBatchTimeAndNoSideEffects(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 4, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 9}}}); err != nil {
		t.Fatal(err)
	}
	before := x.Snapshot()
	if err := x.ValidateBatch(Batch{Now: 4, Ops: []Op{{Put, "b", 8}}}); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if err := x.ValidateBatch(Batch{Now: -1, Ops: []Op{{Put, "b", 8}}}); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if got := x.Snapshot(); got.Now != before.Now || got.NextRevision != before.NextRevision ||
		got.Generation != before.Generation || len(got.Entries) != 1 {
		t.Fatalf("ValidateBatch mutated state: %+v", got)
	}
}

func TestNotFoundAndRollback(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 4, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Ops: []Op{{Touch, "ghost", 5}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := x.Apply(Batch{Ops: []Op{{Delete, "ghost", 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if s := x.Snapshot(); s.Generation != 0 || s.NextRevision != 1 {
		t.Fatalf("failed batch leaked clocks: %+v", s)
	}
}

func TestCapacityRollbackRestoresEvicted(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 2, MaxKeyBytes: 8})
	// "old" expires at 2 and would be evicted by the candidate pass.
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "old", 2}, {Put, "keep", 9}}}); err != nil {
		t.Fatal(err)
	}
	before := x.Snapshot()
	// Batch evicts "old", adds two entries -> final size 3 > 2, must roll back.
	_, err := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "x1", 8}, {Put, "x2", 8}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	after := x.Snapshot()
	if after.Now != before.Now || after.NextRevision != before.NextRevision ||
		after.Generation != before.Generation || len(after.Entries) != 2 {
		t.Fatalf("rollback mismatch: %+v vs %+v", before, after)
	}
}

func TestEvictionOnApplyClosedInterval(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 4, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 4}, {Put, "b", 5}}}); err != nil {
		t.Fatal(err)
	}
	// Now=4 evicts "a" (ExpiresAt <= Now) before ops run.
	if _, err := x.Apply(Batch{Now: 4, Ops: []Op{{Put, "c", 9}}}); err != nil {
		t.Fatal(err)
	}
	s := x.Snapshot()
	if len(s.Entries) != 2 || s.Entries[0].Key != "b" || s.Entries[1].Key != "c" {
		t.Fatalf("%+v", s)
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 4, MaxKeyBytes: 8})
	r1, _ := x.Apply(Batch{Ops: []Op{{Put, "a", 5}}})
	r2, _ := x.Apply(Batch{Now: 1})
	r3, _ := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "a", 7}}})
	if r1.Generation != 1 || r2.Generation != 1 || r3.Generation != 2 {
		t.Fatalf("%v %v %v", r1, r2, r3)
	}
	if x.Snapshot().Now != 2 {
		t.Fatal("empty batch must still advance time")
	}
}

func TestExpireMonotonicAndIsolation(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 4, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 3}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := x.Expire(0); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	gone, err := x.Expire(3)
	if err != nil || len(gone) != 1 {
		t.Fatal(err, gone)
	}
	gone[0].Key = "mutated"
	if x.Snapshot().Now != 3 {
		t.Fatal("expire must advance time")
	}
	s := x.Snapshot()
	s.Entries = append(s.Entries, Entry{Key: "zz", ExpiresAt: 1, Revision: 1})
	if len(x.Snapshot().Entries) != 0 {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestCloneIndependentClocks(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 4, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 9}}}); err != nil {
		t.Fatal(err)
	}
	c, err := x.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != x.Stats() {
		t.Fatal("clone must preserve logical clocks")
	}
	if _, err := c.Apply(Batch{Now: 3, Ops: []Op{{Put, "b", 9}}}); err != nil {
		t.Fatal(err)
	}
	if x.Stats().Entries != 1 || x.Stats().Generation != 1 || x.Stats().Now != 2 {
		t.Fatal("clone mutation leaked into original")
	}
}

func TestConcurrentMixed(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 128, MaxKeyBytes: 16})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			k := fmt.Sprintf("k-%d", i)
			now := int64(i)
			_, _ = x.Apply(Batch{Now: now, Ops: []Op{{Put, k, now + 100}}})
			_ = x.ValidateBatch(Batch{Now: now, Ops: []Op{{Touch, k, now + 200}}})
			_ = x.Stats()
			_ = x.Snapshot()
			if i%4 == 0 {
				_, _ = x.Expire(now)
			}
			if i%8 == 0 {
				_, _ = x.Clone()
			}
		}()
	}
	wg.Wait()
	s := x.Stats()
	if int(s.Entries) != len(x.Snapshot().Entries) {
		t.Fatal("stats and snapshot disagree")
	}
}
