package allocator

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"sync"
	"testing"
)

func mustNew(t *testing.T, o Options) *Allocator {
	t.Helper()
	a, err := New(o)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a
}

func TestAlignmentAndFragmentationExt(t *testing.T) {
	a := mustNew(t, Options{Size: 32, MaxAllocations: 16, MaxNameBytes: 16})
	if _, err := a.Apply(Batch{Ops: []Op{
		reserve("r0", 0, 1),
		reserve("r1", 5, 3),
		reserve("r2", 16, 8),
	}}); err != nil {
		t.Fatal(err)
	}
	// Lowest aligned start: gap [1,5) fits 2-aligned len 2 at 2.
	start, ok, err := a.Find(2, 2)
	if err != nil || !ok || start != 2 {
		t.Fatalf("start=%d ok=%v err=%v", start, ok, err)
	}
	// 8-aligned len 8: gap [8,16) gives 8.
	start, ok, err = a.Find(8, 8)
	if err != nil || !ok || start != 8 {
		t.Fatalf("start=%d ok=%v err=%v", start, ok, err)
	}
	// 16-aligned len 8: only 0.. but occupied; tail gap [24,32) gives 24? 24 is not 16-aligned; 32 out of range -> no space.
	if _, ok, _ = a.Find(8, 16); ok {
		t.Fatal("expected no space for 16-aligned len 8")
	}
	// Alignment larger than size with small length fits at 0 if free.
	b := mustNew(t, Options{Size: 4, MaxAllocations: 4, MaxNameBytes: 8})
	s, ok, err := b.Find(1, 1<<40)
	if err != nil || !ok || s != 0 {
		t.Fatalf("s=%d ok=%v err=%v", s, ok, err)
	}
	// Invalid Find inputs.
	for _, al := range []uint64{0, 3, 6, 12} {
		if _, _, err := a.Find(1, al); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("align=%d err=%v", al, err)
		}
	}
	if _, _, err := a.Find(0, 1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestOverflowBoundaries(t *testing.T) {
	a := mustNew(t, Options{Size: math.MaxUint64, MaxAllocations: 16, MaxNameBytes: 16})
	// Reserve ending exactly at Size is valid.
	if _, err := a.Apply(Batch{Ops: []Op{reserve("top", math.MaxUint64-3, 3)}}); err != nil {
		t.Fatal(err)
	}
	// Start+Length overflows uint64.
	if _, err := a.Apply(Batch{Ops: []Op{reserve("ovf", math.MaxUint64-1, 2)}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := a.Apply(Batch{Ops: []Op{reserve("ovf2", math.MaxUint64, 1)}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	// Huge alignment near 2^63 must not overflow alignUp.
	small := mustNew(t, Options{Size: 1 << 62, MaxAllocations: 4, MaxNameBytes: 8})
	s, ok, err := small.Find(1, 1<<63)
	if err != nil || !ok || s != 0 {
		t.Fatalf("s=%d ok=%v err=%v", s, ok, err)
	}
	// With 0 taken, the next 2^62-aligned start equals Size: no space.
	if _, err := small.Apply(Batch{Ops: []Op{reserve("half", 0, 1)}}); err != nil {
		t.Fatal(err)
	}
	if _, err := small.Apply(Batch{Ops: []Op{allocate("big", 1, 1<<62)}}); !errors.Is(err, ErrNoSpace) {
		t.Fatal(err)
	}
	if _, ok, err := small.Find(1, 1<<62); err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	// Length exceeding size -> ErrNoSpace, not overflow.
	c := mustNew(t, Options{Size: 8, MaxAllocations: 4, MaxNameBytes: 8})
	if _, err := c.Apply(Batch{Ops: []Op{allocate("huge", math.MaxUint64, 1)}}); !errors.Is(err, ErrNoSpace) {
		t.Fatal(err)
	}
}

func TestOverlapVariants(t *testing.T) {
	a := mustNew(t, Options{Size: 64, MaxAllocations: 16, MaxNameBytes: 16})
	if _, err := a.Apply(Batch{Ops: []Op{reserve("a", 8, 8)}}); err != nil {
		t.Fatal(err)
	}
	cases := []Op{
		reserve("b", 7, 2),  // overlaps left edge
		reserve("b", 15, 2), // overlaps right edge
		reserve("b", 8, 8),  // exact match
		reserve("b", 10, 2), // contained
		reserve("b", 4, 12), // covering
	}
	for _, op := range cases {
		if _, err := a.Apply(Batch{Ops: []Op{op}}); !errors.Is(err, ErrConflict) {
			t.Fatalf("op=%+v err=%v", op, err)
		}
	}
	// Touching neighbors are fine.
	if _, err := a.Apply(Batch{Ops: []Op{reserve("l", 4, 4), reserve("r", 16, 4)}}); err != nil {
		t.Fatal(err)
	}
	// Same-batch overlap between two new reserves.
	if _, err := a.Apply(Batch{Ops: []Op{reserve("x", 32, 8), reserve("y", 36, 8)}}); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
}

func TestRevisionRollbackOnFailure(t *testing.T) {
	a := mustNew(t, Options{Size: 64, MaxAllocations: 16, MaxNameBytes: 16})
	if _, err := a.Apply(Batch{Ops: []Op{reserve("a", 0, 4)}}); err != nil {
		t.Fatal(err)
	}
	before := a.Snapshot()
	// Batch allocates revisions then fails on missing free.
	_, err := a.Apply(Batch{Ops: []Op{
		reserve("b", 8, 4),
		allocate("c", 4, 4),
		free("ghost"),
	}})
	if !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	after := a.Snapshot()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("before=%+v after=%+v", before, after)
	}
	// Next successful allocation must reuse the unconsumed revision 2.
	r, err := a.Apply(Batch{Ops: []Op{reserve("b", 8, 4)}})
	if err != nil || r.Revision != 2 {
		t.Fatalf("r=%+v err=%v", r, err)
	}
	b, _, _ := a.Lookup("b")
	if b.Revision != 2 {
		t.Fatalf("b=%+v", b)
	}
}

func TestFreeThenAllocateSameBatch(t *testing.T) {
	a := mustNew(t, Options{Size: 16, MaxAllocations: 4, MaxNameBytes: 8})
	if _, err := a.Apply(Batch{Ops: []Op{reserve("a", 0, 16)}}); err != nil {
		t.Fatal(err)
	}
	// Free then re-reserve the same name and space in one batch.
	r, err := a.Apply(Batch{Ops: []Op{free("a"), reserve("a", 4, 4)}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Changed) != 1 || r.Changed[0].Start != 4 || r.Changed[0].Revision != 2 {
		t.Fatalf("changed=%+v", r.Changed)
	}
	// Allocate into the hole left by a free in the same batch.
	if _, err := a.Apply(Batch{Ops: []Op{reserve("b", 8, 8)}}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Apply(Batch{Ops: []Op{free("b"), allocate("c", 8, 8)}}); err != nil {
		t.Fatal(err)
	}
	c, _, _ := a.Lookup("c")
	if c.Start != 8 {
		t.Fatalf("c=%+v", c)
	}
	// Reserve-then-free in one batch leaves nothing in Changed.
	r, err = a.Apply(Batch{Ops: []Op{reserve("tmp", 0, 1), free("tmp")}})
	if err != nil || len(r.Changed) != 0 {
		t.Fatalf("r=%+v err=%v", r, err)
	}
	if _, ok, _ := a.Lookup("tmp"); ok {
		t.Fatal("tmp should not survive")
	}
}

func TestFinalCapacityTransientOverflow(t *testing.T) {
	a := mustNew(t, Options{Size: 64, MaxAllocations: 2, MaxNameBytes: 8})
	if _, err := a.Apply(Batch{Ops: []Op{reserve("a", 0, 1), reserve("b", 2, 1)}}); err != nil {
		t.Fatal(err)
	}
	// Transiently 3 allocations but final count is 2: allowed.
	if _, err := a.Apply(Batch{Ops: []Op{reserve("c", 4, 1), free("a")}}); err != nil {
		t.Fatal(err)
	}
	// Final count 3: rejected and rolled back.
	before := a.Snapshot()
	if _, err := a.Apply(Batch{Ops: []Op{reserve("d", 6, 1)}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, a.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
}

func TestFindHasNoSideEffects(t *testing.T) {
	a := mustNew(t, Options{Size: 16, MaxAllocations: 4, MaxNameBytes: 8})
	if _, err := a.Apply(Batch{Ops: []Op{reserve("a", 0, 4)}}); err != nil {
		t.Fatal(err)
	}
	before := a.Snapshot()
	for i := 0; i < 3; i++ {
		s, ok, err := a.Find(4, 4)
		if err != nil || !ok || s != 4 {
			t.Fatalf("s=%d ok=%v err=%v", s, ok, err)
		}
	}
	if !reflect.DeepEqual(before, a.Snapshot()) {
		t.Fatal("Find mutated state")
	}
	// Full space -> not found.
	full := mustNew(t, Options{Size: 4, MaxAllocations: 4, MaxNameBytes: 8})
	if _, err := full.Apply(Batch{Ops: []Op{reserve("x", 0, 4)}}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := full.Find(1, 1); err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestSnapshotAndChangedOrdering(t *testing.T) {
	a := mustNew(t, Options{Size: 64, MaxAllocations: 16, MaxNameBytes: 16})
	r, err := a.Apply(Batch{Ops: []Op{
		reserve("z", 40, 4),
		reserve("m", 8, 4),
		reserve("a", 20, 4),
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"m", "a", "z"}
	if len(r.Changed) != 3 {
		t.Fatalf("changed=%+v", r.Changed)
	}
	for i, name := range want {
		if r.Changed[i].Name != name {
			t.Fatalf("changed[%d]=%+v", i, r.Changed[i])
		}
	}
	snap := a.Snapshot()
	for i, name := range want {
		if snap.Allocations[i].Name != name {
			t.Fatalf("snap[%d]=%+v", i, snap.Allocations[i])
		}
	}
	// Mutating returned slices must not affect the allocator.
	snap.Allocations[0].Name = "corrupt"
	snap.Allocations[0].Start = 63
	r.Changed[0].Length = 99
	snap2 := a.Snapshot()
	if snap2.Allocations[0].Name != "m" || snap2.Allocations[0].Start != 8 {
		t.Fatalf("snap=%+v", snap2.Allocations[0])
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	a := mustNew(t, Options{Size: 8, MaxAllocations: 4, MaxNameBytes: 8})
	r, err := a.Apply(Batch{})
	if err != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatalf("r=%+v err=%v", r, err)
	}
	if _, err := a.Apply(Batch{Ops: []Op{reserve("a", 0, 1)}}); err != nil {
		t.Fatal(err)
	}
	r, err = a.Apply(Batch{})
	if err != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatalf("r=%+v err=%v", r, err)
	}
	// Failed batch does not bump generation.
	if _, err := a.Apply(Batch{Ops: []Op{free("nope")}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if a.Snapshot().Generation != 1 {
		t.Fatalf("gen=%d", a.Snapshot().Generation)
	}
}

func TestLookupValidation(t *testing.T) {
	a := mustNew(t, Options{Size: 8, MaxAllocations: 4, MaxNameBytes: 4})
	for _, bad := range []string{"", "toolongname", "bad name", "bad?", "é"} {
		if _, _, err := a.Lookup(bad); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("name=%q err=%v", bad, err)
		}
	}
	if _, ok, err := a.Lookup("ok-1"); err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestConcurrentMixedOperations(t *testing.T) {
	a := mustNew(t, Options{Size: 4096, MaxAllocations: 256, MaxNameBytes: 24})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("w-%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = a.Apply(Batch{Ops: []Op{allocate(name, 2, 2)}})
				_, _, _ = a.Lookup(name)
				_, _, _ = a.Find(2, 2)
				_ = a.Snapshot()
				_, _ = a.Apply(Batch{Ops: []Op{free(name)}})
			}
		}()
	}
	wg.Wait()
	snap := a.Snapshot()
	for i := 1; i < len(snap.Allocations); i++ {
		prev, cur := snap.Allocations[i-1], snap.Allocations[i]
		if prev.Start+prev.Length > cur.Start {
			t.Fatalf("overlap: %+v vs %+v", prev, cur)
		}
	}
	seen := map[string]bool{}
	revs := map[uint64]bool{}
	for _, al := range snap.Allocations {
		if seen[al.Name] {
			t.Fatalf("duplicate name %q", al.Name)
		}
		seen[al.Name] = true
		if revs[al.Revision] {
			t.Fatalf("duplicate revision %d", al.Revision)
		}
		revs[al.Revision] = true
	}
}
