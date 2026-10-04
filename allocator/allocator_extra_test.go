package allocator

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"sync"
	"testing"
)

func xalloc(t *testing.T, o Options) *Allocator {
	t.Helper()
	a, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestOptionsInvalid(t *testing.T) {
	for _, o := range []Options{
		{Size: 0, MaxAllocations: 1, MaxNameBytes: 1},
		{Size: 1, MaxAllocations: 0, MaxNameBytes: 1},
		{Size: 1, MaxAllocations: 1, MaxNameBytes: 0},
		{Size: 1, MaxAllocations: -1, MaxNameBytes: 1},
	} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("o=%+v err=%v", o, err)
		}
	}
}

func TestAlignmentAndFragmentationExtra(t *testing.T) {
	a := xalloc(t, Options{Size: 32, MaxAllocations: 16, MaxNameBytes: 16})
	if _, err := a.Apply(Batch{Ops: []Op{
		{Kind: Reserve, Name: "r1", Start: 0, Length: 1},
		{Kind: Reserve, Name: "r2", Start: 9, Length: 7},
	}}); err != nil {
		t.Fatal(err)
	}
	// lowest 8-aligned start fitting length 4: 16 (gap 1..9 too small at 8)
	r, err := a.Apply(Batch{Ops: []Op{{Kind: Allocate, Name: "a", Length: 4, Alignment: 8}}})
	if err != nil {
		t.Fatal(err)
	}
	al, _, _ := a.Lookup("a")
	if al.Start != 16 {
		t.Fatalf("start=%d", al.Start)
	}
	if len(r.Changed) != 1 || r.Changed[0].Name != "a" {
		t.Fatalf("changed=%+v", r.Changed)
	}
	// exact-fit at end of space
	if _, err := a.Apply(Batch{Ops: []Op{{Kind: Allocate, Name: "tail", Length: 12, Alignment: 4}}}); err != nil {
		t.Fatal(err)
	}
	tl, _, _ := a.Lookup("tail")
	if tl.Start != 20 {
		t.Fatalf("tail=%+v", tl)
	}
	// invalid alignment / length
	if _, _, err := a.Find(1, 3); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, _, err := a.Find(0, 1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := a.Apply(Batch{Ops: []Op{{Kind: Allocate, Name: "bad", Length: 2, Alignment: 12}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestOverflowBoundaries(t *testing.T) {
	a := xalloc(t, Options{Size: math.MaxUint64, MaxAllocations: 8, MaxNameBytes: 16})
	// Reserve reaching the very top of the space.
	if _, err := a.Apply(Batch{Ops: []Op{{Kind: Reserve, Name: "top", Start: math.MaxUint64 - 4, Length: 4}}}); err != nil {
		t.Fatal(err)
	}
	// Overflowing reserve rejected structurally.
	if _, err := a.Apply(Batch{Ops: []Op{{Kind: Reserve, Name: "ovf", Start: math.MaxUint64, Length: 1}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	// Huge alignment allocation must not overflow and must find 0... no, 0 is free and aligned.
	r, err := a.Apply(Batch{Ops: []Op{{Kind: Allocate, Name: "big", Length: 1, Alignment: 1 << 62}}})
	if err != nil {
		t.Fatal(err)
	}
	al, _, _ := a.Lookup("big")
	if al.Start != 0 || r.Revision != 2 {
		t.Fatalf("al=%+v r=%+v", al, r)
	}
	// Next huge-aligned allocation lands at 1<<62.
	if _, err := a.Apply(Batch{Ops: []Op{{Kind: Allocate, Name: "big2", Length: 1, Alignment: 1 << 62}}}); err != nil {
		t.Fatal(err)
	}
	al2, _, _ := a.Lookup("big2")
	if al2.Start != 1<<62 {
		t.Fatalf("al2=%+v", al2)
	}
	// Alignment 1<<63 with occupied 0: next candidate 1<<63 must still work.
	if _, err := a.Apply(Batch{Ops: []Op{{Kind: Allocate, Name: "big3", Length: 1, Alignment: 1 << 63}}}); err != nil {
		t.Fatal(err)
	}
	al3, _, _ := a.Lookup("big3")
	if al3.Start != 1<<63 {
		t.Fatalf("al3=%+v", al3)
	}
}

func TestOverlapVariants(t *testing.T) {
	a := xalloc(t, Options{Size: 16, MaxAllocations: 8, MaxNameBytes: 16})
	if _, err := a.Apply(Batch{Ops: []Op{{Kind: Reserve, Name: "m", Start: 4, Length: 4}}}); err != nil {
		t.Fatal(err)
	}
	for _, op := range []Op{
		{Kind: Reserve, Name: "o1", Start: 3, Length: 2},  // overlaps left edge
		{Kind: Reserve, Name: "o2", Start: 7, Length: 2},  // overlaps right edge
		{Kind: Reserve, Name: "o3", Start: 4, Length: 4},  // identical
		{Kind: Reserve, Name: "o4", Start: 0, Length: 16}, // covers
		{Kind: Reserve, Name: "o5", Start: 5, Length: 1},  // inside
	} {
		if _, err := a.Apply(Batch{Ops: []Op{op}}); !errors.Is(err, ErrConflict) {
			t.Fatalf("op=%+v err=%v", op, err)
		}
	}
	// Adjacent (touching) ranges are fine.
	if _, err := a.Apply(Batch{Ops: []Op{
		{Kind: Reserve, Name: "l", Start: 0, Length: 4},
		{Kind: Reserve, Name: "r", Start: 8, Length: 4},
	}}); err != nil {
		t.Fatal(err)
	}
}

func TestFreeThenAllocateSameBatch(t *testing.T) {
	a := xalloc(t, Options{Size: 16, MaxAllocations: 4, MaxNameBytes: 16})
	if _, err := a.Apply(Batch{Ops: []Op{{Kind: Reserve, Name: "x", Start: 0, Length: 16}}}); err != nil {
		t.Fatal(err)
	}
	// Free then re-reserve the exact same range and name reuse in one batch.
	r, err := a.Apply(Batch{Ops: []Op{
		{Kind: Free, Name: "x"},
		{Kind: Reserve, Name: "x", Start: 4, Length: 4},
		{Kind: Allocate, Name: "y", Length: 4, Alignment: 4},
	}})
	if err != nil {
		t.Fatal(err)
	}
	x, _, _ := a.Lookup("x")
	y, _, _ := a.Lookup("y")
	if x.Start != 4 || x.Revision != 2 || y.Start != 0 || y.Revision != 3 {
		t.Fatalf("x=%+v y=%+v", x, y)
	}
	// Changed excludes freed entries, includes survivors sorted by Start.
	if len(r.Changed) != 2 || r.Changed[0].Name != "y" || r.Changed[1].Name != "x" {
		t.Fatalf("changed=%+v", r.Changed)
	}
	// Free of a name freed earlier in the same batch fails.
	if _, err := a.Apply(Batch{Ops: []Op{{Kind: Free, Name: "y"}, {Kind: Free, Name: "y"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestRevisionRollback(t *testing.T) {
	a := xalloc(t, Options{Size: 16, MaxAllocations: 8, MaxNameBytes: 16})
	if _, err := a.Apply(Batch{Ops: []Op{{Kind: Reserve, Name: "a", Start: 0, Length: 1}}}); err != nil {
		t.Fatal(err)
	}
	before := a.Snapshot()
	// Batch fails at the end: revisions consumed inside must roll back.
	_, err := a.Apply(Batch{Ops: []Op{
		{Kind: Reserve, Name: "b", Start: 2, Length: 1},
		{Kind: Allocate, Name: "c", Length: 1, Alignment: 1},
		{Kind: Free, Name: "missing"},
	}})
	if !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	after := a.Snapshot()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("before=%+v after=%+v", before, after)
	}
	// Next successful allocation gets revision 2, not 4.
	r, err := a.Apply(Batch{Ops: []Op{{Kind: Reserve, Name: "b", Start: 2, Length: 1}}})
	if err != nil || r.Revision != 2 {
		t.Fatalf("r=%+v err=%v", r, err)
	}
	// Generation rolled back too: this was the second successful nonempty batch.
	if r.Generation != 2 {
		t.Fatalf("gen=%d", r.Generation)
	}
}

func TestFinalCapacityTransientOverflow(t *testing.T) {
	a := xalloc(t, Options{Size: 64, MaxAllocations: 2, MaxNameBytes: 16})
	if _, err := a.Apply(Batch{Ops: []Op{{Kind: Reserve, Name: "a", Start: 0, Length: 1}}}); err != nil {
		t.Fatal(err)
	}
	// Transiently 3 allocations, ends at 2: allowed.
	r, err := a.Apply(Batch{Ops: []Op{
		{Kind: Reserve, Name: "b", Start: 2, Length: 1},
		{Kind: Reserve, Name: "c", Start: 4, Length: 1},
		{Kind: Free, Name: "b"},
	}})
	if err != nil || len(a.Snapshot().Allocations) != 2 {
		t.Fatalf("r=%+v err=%v", r, err)
	}
	// Ends at 3: capacity error, full rollback including revision.
	before := a.Snapshot()
	if _, err := a.Apply(Batch{Ops: []Op{
		{Kind: Reserve, Name: "d", Start: 6, Length: 1},
		{Kind: Reserve, Name: "e", Start: 8, Length: 1},
	}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, a.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
}

func TestFindNoSideEffects(t *testing.T) {
	a := xalloc(t, Options{Size: 16, MaxAllocations: 4, MaxNameBytes: 16})
	if _, err := a.Apply(Batch{Ops: []Op{{Kind: Reserve, Name: "a", Start: 0, Length: 8}}}); err != nil {
		t.Fatal(err)
	}
	before := a.Snapshot()
	start, ok, err := a.Find(4, 4)
	if err != nil || !ok || start != 8 {
		t.Fatalf("start=%d ok=%v err=%v", start, ok, err)
	}
	if _, ok, err := a.Find(9, 1); err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if _, ok, err := a.Find(32, 1); !errors.Is(err, ErrInvalidInput) || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if !reflect.DeepEqual(before, a.Snapshot()) {
		t.Fatal("Find mutated state")
	}
}

func TestSnapshotSortingAndIsolation(t *testing.T) {
	a := xalloc(t, Options{Size: 64, MaxAllocations: 8, MaxNameBytes: 16})
	if _, err := a.Apply(Batch{Ops: []Op{
		{Kind: Reserve, Name: "z", Start: 32, Length: 4},
		{Kind: Reserve, Name: "a", Start: 0, Length: 4},
		{Kind: Reserve, Name: "m", Start: 16, Length: 4},
	}}); err != nil {
		t.Fatal(err)
	}
	s := a.Snapshot()
	names := []string{s.Allocations[0].Name, s.Allocations[1].Name, s.Allocations[2].Name}
	if !reflect.DeepEqual(names, []string{"a", "m", "z"}) {
		t.Fatalf("names=%v", names)
	}
	if s.Generation != 1 || s.NextRevision != 4 || s.Size != 64 {
		t.Fatalf("s=%+v", s)
	}
	// Mutating the returned slice must not affect the allocator.
	s.Allocations[0].Start = 100
	s2 := a.Snapshot()
	if s2.Allocations[0].Start != 0 {
		t.Fatal("snapshot not isolated")
	}
	// Empty batch: no generation bump, revision reflects latest.
	r, err := a.Apply(Batch{})
	if err != nil || r.Generation != 1 || r.Revision != 3 || len(r.Changed) != 0 {
		t.Fatalf("r=%+v err=%v", r, err)
	}
	if a.Snapshot().Generation != 1 {
		t.Fatal("empty batch changed generation")
	}
}

func TestLookupValidation(t *testing.T) {
	a := xalloc(t, Options{Size: 8, MaxAllocations: 2, MaxNameBytes: 4})
	if _, _, err := a.Lookup("bad name!"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, _, err := a.Lookup("toolongname"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, ok, err := a.Lookup("none"); err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestConcurrentMixed(t *testing.T) {
	a := xalloc(t, Options{Size: 4096, MaxAllocations: 256, MaxNameBytes: 24})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("w-%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = a.Apply(Batch{Ops: []Op{{Kind: Allocate, Name: name, Length: 2, Alignment: 2}}})
				_, _, _ = a.Find(1, 1)
				_, _, _ = a.Lookup(name)
				_ = a.Snapshot()
				_, _ = a.Apply(Batch{Ops: []Op{{Kind: Free, Name: name}}})
			}
		}()
	}
	wg.Wait()
	s := a.Snapshot()
	seen := map[string]bool{}
	for _, al := range s.Allocations {
		if seen[al.Name] {
			t.Fatalf("duplicate name %q", al.Name)
		}
		seen[al.Name] = true
		if al.Start%2 != 0 {
			t.Fatalf("unaligned %+v", al)
		}
	}
	for i := 1; i < len(s.Allocations); i++ {
		prev, cur := s.Allocations[i-1], s.Allocations[i]
		if prev.Start+prev.Length > cur.Start {
			t.Fatalf("overlap %+v %+v", prev, cur)
		}
	}
}
