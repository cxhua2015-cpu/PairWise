package expirytable374

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 1}, {1, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatal(o, e)
		}
	}
}

func TestKeyCharset(t *testing.T) {
	x := table(t)
	for _, k := range []string{"", "A", "a b", "a.b", "a/b", "toolongkey"} {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatal(k, e)
		}
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "ok_key-1", 1}}}); e != nil {
		t.Fatal(e)
	}
}

func TestUnknownKindAndNegative(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(9), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: -1, Ops: []Op{{Put, "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestExpiryAtNowBoundary(t *testing.T) {
	x := table(t)
	// Expiry runs before the ops, so a pre-existing entry with
	// ExpiresAt <= Now is dropped, while a fresh Put at ExpiresAt == Now lands.
	_, _ = x.Apply(Batch{Now: 4, Ops: []Op{{Put, "old", 5}}})
	r, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}})
	if e != nil || r.Generation != 2 {
		t.Fatal(e, r)
	}
	s := x.Snapshot()
	if len(s.Entries) != 2 || s.Entries[0].Key != "a" || s.Entries[1].Key != "b" {
		t.Fatal(s.Entries)
	}
	// Expire uses the same closed boundary: ExpiresAt <= now is removed.
	gone, e := x.Expire(5)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
}

func TestNotFoundAndRollback(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 10}}})
	b := x.Snapshot()
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "ghost", 3}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Delete, "ghost", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// Touch of an entry that expired at Now must fail and roll back everything.
	if _, e := x.Apply(Batch{Now: 10, Ops: []Op{{Touch, "a", 20}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if got := x.Snapshot(); got.Now != b.Now || got.Generation != b.Generation ||
		got.NextRevision != b.NextRevision || len(got.Entries) != 1 {
		t.Fatal(got)
	}
}

func TestCapacityRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 100}}})
	b := x.Snapshot()
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 3}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	got := x.Snapshot()
	if got.Now != b.Now || got.Generation != b.Generation || got.NextRevision != b.NextRevision ||
		len(got.Entries) != 1 || got.Entries[0].Key != "a" {
		t.Fatal(got)
	}
	// Expiry inside the batch frees room before the capacity check.
	if _, e = x.Apply(Batch{Now: 100, Ops: []Op{{Put, "b", 200}}}); e != nil {
		t.Fatal(e)
	}
	if k := x.Snapshot().Entries[0].Key; k != "b" {
		t.Fatal(k)
	}
}

func TestGenerationAndRevisionCounters(t *testing.T) {
	x := table(t)
	r, _ := x.Apply(Batch{Ops: nil})
	if r.Generation != 0 || x.Snapshot().Generation != 0 {
		t.Fatal(r)
	}
	r, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}, {Put, "b", 9}, {Touch, "a", 10}}})
	if r.Generation != 1 || r.Revision != 3 {
		t.Fatal(r)
	}
	if s := x.Snapshot(); s.NextRevision != 4 {
		t.Fatal(s)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	s := x.Snapshot()
	s.Entries[0].ExpiresAt = 1
	if x.Snapshot().Entries[0].ExpiresAt != 9 {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestExpireMonotonicTime(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 9}}})
	if _, e := x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 4, Ops: []Op{{Put, "b", 9}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
}

func TestConcurrentMixed(t *testing.T) {
	x, _ := New(Options{MaxEntries: 128, MaxKeyBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("key-%d", i)
			for n := int64(0); n < 50; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Touch, k, n + 100}}})
				_, _ = x.Expire(n)
				_ = x.Snapshot()
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if len(s.Entries) > 128 {
		t.Fatal(len(s.Entries))
	}
}
