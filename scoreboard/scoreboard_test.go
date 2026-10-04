package scoreboard

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"
)

func TestRepeatedOpsInBatch(t *testing.T) {
	b, _ := New(Options{MaxMembers: 4, MaxNameBytes: 8})
	x, err := b.Apply(Batch{Ops: []Op{
		{Kind: Upsert, Member: "a", Score: 1},
		{Kind: Upsert, Member: "a", Score: 10},
		{Kind: Increment, Member: "a", Delta: 5},
		{Kind: Increment, Member: "a", Delta: -2},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if x.Revision != 4 || x.Generation != 1 {
		t.Fatalf("x=%+v", x)
	}
	if len(x.Changed) != 1 || x.Changed[0].Score != 13 || x.Changed[0].Revision != 4 {
		t.Fatalf("changed=%+v", x.Changed)
	}
	e, ok, _ := b.Get("a")
	if !ok || e.Score != 13 || e.Revision != 4 {
		t.Fatalf("e=%+v ok=%v", e, ok)
	}
}

func TestDeleteRecreateInBatch(t *testing.T) {
	b, _ := New(Options{MaxMembers: 4, MaxNameBytes: 8})
	if _, err := b.Apply(Batch{Ops: []Op{{Kind: Upsert, Member: "a", Score: 7}}}); err != nil {
		t.Fatal(err)
	}
	x, err := b.Apply(Batch{Ops: []Op{
		{Kind: Delete, Member: "a"},
		{Kind: Upsert, Member: "a", Score: 3},
		{Kind: Increment, Member: "a", Delta: 1},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if x.Revision != 3 || len(x.Changed) != 1 || x.Changed[0].Score != 4 || x.Changed[0].Revision != 3 {
		t.Fatalf("x=%+v", x)
	}
	// Delete after upsert in same batch: member absent from Changed.
	x, err = b.Apply(Batch{Ops: []Op{
		{Kind: Upsert, Member: "b", Score: 9},
		{Kind: Delete, Member: "b"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(x.Changed) != 0 || x.Revision != 4 {
		t.Fatalf("x=%+v", x)
	}
	if _, ok, _ := b.Get("b"); ok {
		t.Fatal("b should not exist")
	}
}

func TestRevisionRollbackOnError(t *testing.T) {
	b, _ := New(Options{MaxMembers: 4, MaxNameBytes: 8})
	if _, err := b.Apply(Batch{Ops: []Op{{Kind: Upsert, Member: "a", Score: 1}}}); err != nil {
		t.Fatal(err)
	}
	before := b.Snapshot()
	// Batch allocates revisions for the first two ops, then fails.
	_, err := b.Apply(Batch{Ops: []Op{
		{Kind: Upsert, Member: "b", Score: 2},
		{Kind: Increment, Member: "a", Delta: 1},
		{Kind: Delete, Member: "ghost"},
	}})
	if !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	after := b.Snapshot()
	if after.NextRevision != before.NextRevision || after.Generation != before.Generation || len(after.Entries) != 1 {
		t.Fatalf("before=%+v after=%+v", before, after)
	}
	// Next successful batch continues from the un-consumed revision.
	x, err := b.Apply(Batch{Ops: []Op{{Kind: Upsert, Member: "b", Score: 2}}})
	if err != nil || x.Revision != 2 {
		t.Fatalf("x=%+v err=%v", x, err)
	}
}

func TestExtremeArithmetic(t *testing.T) {
	b, _ := New(Options{MaxMembers: 4, MaxNameBytes: 8})
	if _, err := b.Apply(Batch{Ops: []Op{
		{Kind: Upsert, Member: "hi", Score: math.MaxInt64},
		{Kind: Upsert, Member: "lo", Score: math.MinInt64},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Apply(Batch{Ops: []Op{{Kind: Increment, Member: "hi", Delta: 1}}}); !errors.Is(err, ErrOverflow) {
		t.Fatal(err)
	}
	if _, err := b.Apply(Batch{Ops: []Op{{Kind: Increment, Member: "lo", Delta: -1}}}); !errors.Is(err, ErrOverflow) {
		t.Fatal(err)
	}
	// Exact boundary additions are allowed.
	if _, err := b.Apply(Batch{Ops: []Op{{Kind: Increment, Member: "hi", Delta: -1}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Apply(Batch{Ops: []Op{{Kind: Increment, Member: "lo", Delta: 1}}}); err != nil {
		t.Fatal(err)
	}
	// Minus MinInt64 magnitude via two steps still cannot overflow silently.
	if _, err := b.Apply(Batch{Ops: []Op{{Kind: Increment, Member: "lo", Delta: math.MinInt64}}}); !errors.Is(err, ErrOverflow) {
		t.Fatal(err)
	}
	e, _, _ := b.Get("hi")
	if e.Score != math.MaxInt64-1 {
		t.Fatalf("e=%+v", e)
	}
}

func TestFinalCapacityCheckedLast(t *testing.T) {
	b, _ := New(Options{MaxMembers: 2, MaxNameBytes: 8})
	if _, err := b.Apply(Batch{Ops: []Op{
		{Kind: Upsert, Member: "a", Score: 1},
		{Kind: Upsert, Member: "b", Score: 2},
	}}); err != nil {
		t.Fatal(err)
	}
	// Net-zero batch that transiently exceeds capacity succeeds.
	if _, err := b.Apply(Batch{Ops: []Op{
		{Kind: Upsert, Member: "c", Score: 3},
		{Kind: Delete, Member: "a"},
	}}); err != nil {
		t.Fatal(err)
	}
	// Final count above capacity fails and rolls back everything.
	before := b.Snapshot()
	if _, err := b.Apply(Batch{Ops: []Op{
		{Kind: Delete, Member: "b"},
		{Kind: Upsert, Member: "d", Score: 4},
		{Kind: Upsert, Member: "e", Score: 5},
	}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	after := b.Snapshot()
	if after.Generation != before.Generation || after.NextRevision != before.NextRevision || len(after.Entries) != len(before.Entries) {
		t.Fatalf("before=%+v after=%+v", before, after)
	}
}

func TestRangeCursorBoundaries(t *testing.T) {
	b, _ := New(Options{MaxMembers: 16, MaxNameBytes: 8})
	ops := []Op{}
	for i := 0; i < 6; i++ {
		ops = append(ops, Op{Kind: Upsert, Member: fmt.Sprintf("m%d", i), Score: int64(i * 10)})
	}
	if _, err := b.Apply(Batch{Ops: ops}); err != nil {
		t.Fatal(err)
	}
	// Inclusive bounds.
	x, err := b.Range(10, 30, Cursor{}, 10)
	if err != nil || len(x) != 3 || x[0].Member != "m3" || x[2].Member != "m1" {
		t.Fatalf("x=%+v err=%v", x, err)
	}
	// Cursor on an existing member skips it (strictly after).
	x, err = b.Range(0, 50, Cursor{Set: true, Score: 30, Member: "m3"}, 10)
	if err != nil || len(x) != 3 || x[0].Member != "m2" {
		t.Fatalf("x=%+v err=%v", x, err)
	}
	// Cursor beyond the last entry yields an empty page.
	x, err = b.Range(0, 50, Cursor{Set: true, Score: 0, Member: "m0"}, 10)
	if err != nil || len(x) != 0 {
		t.Fatalf("x=%+v err=%v", x, err)
	}
	// Cursor for a deleted member still paginates by its key.
	if _, err := b.Apply(Batch{Ops: []Op{{Kind: Delete, Member: "m3"}}}); err != nil {
		t.Fatal(err)
	}
	x, err = b.Range(0, 50, Cursor{Set: true, Score: 30, Member: "m3"}, 10)
	if err != nil || len(x) != 3 || x[0].Member != "m2" {
		t.Fatalf("x=%+v err=%v", x, err)
	}
	// Limit boundaries.
	if _, err = b.Range(0, 50, Cursor{}, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err = b.Range(0, 50, Cursor{}, 1001); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	// Full pagination walk with limit 2 collects everything in order.
	var seen []string
	cur := Cursor{}
	for {
		page, err := b.Range(0, 50, cur, 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		for _, e := range page {
			seen = append(seen, e.Member)
		}
		last := page[len(page)-1]
		cur = Cursor{Set: true, Score: last.Score, Member: last.Member}
	}
	if len(seen) != 5 || seen[0] != "m5" || seen[4] != "m0" {
		t.Fatalf("seen=%v", seen)
	}
}

func TestSortTieBreakByMember(t *testing.T) {
	b, _ := New(Options{MaxMembers: 8, MaxNameBytes: 8})
	if _, err := b.Apply(Batch{Ops: []Op{
		{Kind: Upsert, Member: "z", Score: 5},
		{Kind: Upsert, Member: "a", Score: 5},
		{Kind: Upsert, Member: "m", Score: 5},
		{Kind: Upsert, Member: "b", Score: 9},
	}}); err != nil {
		t.Fatal(err)
	}
	s := b.Snapshot()
	want := []string{"b", "a", "m", "z"}
	if len(s.Entries) != len(want) {
		t.Fatalf("s=%+v", s)
	}
	for i, w := range want {
		if s.Entries[i].Member != w {
			t.Fatalf("entries=%+v", s.Entries)
		}
	}
}

func TestSnapshotIsolation(t *testing.T) {
	b, _ := New(Options{MaxMembers: 8, MaxNameBytes: 8})
	if _, err := b.Apply(Batch{Ops: []Op{{Kind: Upsert, Member: "a", Score: 1}}}); err != nil {
		t.Fatal(err)
	}
	s := b.Snapshot()
	s.Entries[0].Score = 999
	x, _ := b.Range(0, 100, Cursor{}, 10)
	x[0].Score = 999
	e, _, _ := b.Get("a")
	if e.Score != 1 {
		t.Fatalf("e=%+v", e)
	}
}

func TestConcurrentMixed(t *testing.T) {
	b, _ := New(Options{MaxMembers: 256, MaxNameBytes: 16})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			m := fmt.Sprintf("n-%02d", i)
			for j := 0; j < 50; j++ {
				_, _ = b.Apply(Batch{Ops: []Op{
					{Kind: Upsert, Member: m, Score: int64(j)},
					{Kind: Increment, Member: m, Delta: 1},
				}})
				_, _, _ = b.Get(m)
				_, _ = b.Range(0, 1000, Cursor{}, 10)
				_ = b.Snapshot()
			}
		}()
	}
	wg.Wait()
	s := b.Snapshot()
	if len(s.Entries) != 32 || s.Generation != 32*50 || s.NextRevision != 32*50*2+1 {
		t.Fatalf("s=%+v gen=%d next=%d", s.Entries[:1], s.Generation, s.NextRevision)
	}
}
