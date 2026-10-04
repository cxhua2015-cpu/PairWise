package rangelock

import (
	"bytes"
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
)

func opts() Options {
	return Options{MaxLocks: 8, MaxOwners: 4, MaxMetadataBytes: 32, MaxNameBytes: 16}
}
func manager(t *testing.T, o Options) *Manager {
	t.Helper()
	m, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestOptionsAndStructuralValidationFirst(t *testing.T) {
	if _, err := New(Options{}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("New=%v", err)
	}
	m := manager(t, opts())
	_, _, _ = m.AcquireBatch(0, []Request{{ID: "x", Owner: "a", Resource: "r", Start: 0, End: 2, Mode: Write, TTL: 3}})
	before := m.Snapshot()
	_, _, err := m.AcquireBatch(4, []Request{{ID: "x", Owner: "b", Resource: "r", Start: 0, End: 1, Mode: Write, TTL: 1}, {ID: "", Owner: "", Resource: "", Start: 1, End: 1, TTL: -1}})
	if !errors.Is(err, ErrInvalidInput) || !reflect.DeepEqual(before, m.Snapshot()) {
		t.Fatalf("err=%v", err)
	}
	_, _, err = m.AcquireBatch(math.MaxInt64, []Request{{ID: "y", Owner: "a", Resource: "r", Start: 0, End: 1, Mode: Read, TTL: 1}})
	if !errors.Is(err, ErrInvalidTime) {
		t.Fatalf("overflow=%v", err)
	}
}

func TestReadCoexistWriteConflictAndAdjacency(t *testing.T) {
	m := manager(t, opts())
	leases, _, err := m.AcquireBatch(0, []Request{{"a", "o1", "r", 0, 10, Read, 5, nil}, {"b", "o2", "r", 5, 15, Read, 5, nil}, {"c", "o3", "r", 15, 20, Write, 5, nil}})
	if err != nil || len(leases) != 3 {
		t.Fatalf("leases=%v err=%v", leases, err)
	}
	before := m.Snapshot()
	if _, _, err = m.AcquireBatch(1, []Request{{"w", "o1", "r", 9, 11, Write, 2, nil}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflict=%v", err)
	}
	if !reflect.DeepEqual(before, m.Snapshot()) {
		t.Fatal("conflict mutated")
	}
}

func TestExpiryTakeoverAndFailedBatchDoesNotConsumeToken(t *testing.T) {
	m := manager(t, opts())
	first, _, _ := m.AcquireBatch(0, []Request{{"id", "old", "r", 0, 5, Write, 3, nil}})
	before := m.Snapshot()
	_, _, err := m.AcquireBatch(3, []Request{{"new", "n", "r", 0, 5, Write, 2, nil}, {"new", "n", "r", 6, 7, Read, 2, nil}})
	if !errors.Is(err, ErrInvalidInput) || !reflect.DeepEqual(before, m.Snapshot()) {
		t.Fatalf("err=%v", err)
	}
	next, _, err := m.AcquireBatch(3, []Request{{"id", "new", "r", 0, 5, Write, 2, nil}})
	if err != nil || next[0].Token != first[0].Token+1 {
		t.Fatalf("next=%v err=%v", next, err)
	}
}

func TestFinalCapacityAfterExpiryPruneAndOwnership(t *testing.T) {
	m := manager(t, Options{MaxLocks: 1, MaxOwners: 1, MaxMetadataBytes: 3, MaxNameBytes: 8})
	in := []byte("old")
	_, _, _ = m.AcquireBatch(0, []Request{{"a", "x", "r", 0, 1, Write, 2, in}})
	in[0] = 'X'
	meta := []byte("new")
	out, _, err := m.AcquireBatch(2, []Request{{"b", "y", "r", 0, 1, Write, 2, meta}})
	if err != nil || string(out[0].Metadata) != "new" {
		t.Fatalf("out=%v err=%v", out, err)
	}
	meta[0] = 'Y'
	out[0].Metadata[0] = 'Z'
	if got := m.Snapshot().Leases[0].Metadata; !bytes.Equal(got, []byte("new")) {
		t.Fatalf("alias=%q", got)
	}
}

func TestRenewReleaseBoundaries(t *testing.T) {
	m := manager(t, opts())
	leases, _, _ := m.AcquireBatch(1, []Request{{"a", "o", "r", 0, 1, Read, 4, nil}})
	token := leases[0].Token
	before := m.Snapshot()
	if err := m.Renew("a", token, 5, 2); !errors.Is(err, ErrStaleToken) {
		t.Fatalf("boundary=%v", err)
	}
	if !reflect.DeepEqual(before, m.Snapshot()) {
		t.Fatal("failed renew mutated")
	}
	if err := m.Release("a", token+1); !errors.Is(err, ErrStaleToken) {
		t.Fatalf("wrong token=%v", err)
	}
	if err := m.Release("a", token); err != nil {
		t.Fatal(err)
	}
}

func TestSweepQuerySnapshotOrdering(t *testing.T) {
	m := manager(t, opts())
	_, _, _ = m.AcquireBatch(0, []Request{{"z", "o1", "b", 10, 20, Read, 4, []byte("z")}, {"a", "o2", "a", 5, 9, Write, 8, []byte("a")}, {"b", "o3", "a", 0, 5, Read, 8, []byte("b")}})
	q, err := m.Query("a", 4, 6, 1)
	if err != nil || len(q) != 2 || q[0].ID != "b" || q[1].ID != "a" {
		t.Fatalf("query=%v err=%v", q, err)
	}
	s := m.Snapshot()
	if len(s.Leases) != 3 || s.Leases[0].ID != "b" || s.Leases[1].ID != "a" || s.Leases[2].ID != "z" {
		t.Fatalf("snapshot=%+v", s)
	}
	ids, err := m.Sweep(4, 1)
	if err != nil || !reflect.DeepEqual(ids, []string{"z"}) {
		t.Fatalf("ids=%v err=%v", ids, err)
	}
}

func TestConcurrentReadLocks(t *testing.T) {
	m := manager(t, Options{MaxLocks: 64, MaxOwners: 64, MaxMetadataBytes: 64, MaxNameBytes: 16})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := string(rune('A' + i))
			if _, _, err := m.AcquireBatch(0, []Request{{id, id, "r", 0, 10, Read, 10, nil}}); err != nil {
				t.Errorf("acquire=%v", err)
			}
			_, _ = m.Query("r", 1, 2, 1)
			_ = m.Snapshot()
		}()
	}
	wg.Wait()
	if s := m.Snapshot(); s.Locks != 32 || s.Owners != 32 {
		t.Fatalf("snapshot=%+v", s)
	}
}
