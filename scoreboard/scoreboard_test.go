package scoreboard

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"sync"
	"testing"
)

func TestRepeatedOpsInBatch(t *testing.T) {
	b := board(t)
	x, e := b.Apply(Batch{Ops: []Op{
		up("a", 1), up("a", 5), inc("a", 2), inc("a", -1),
	}})
	if e != nil {
		t.Fatal(e)
	}
	if x.Revision != 4 || x.Generation != 1 {
		t.Fatalf("x=%+v", x)
	}
	if len(x.Changed) != 1 || x.Changed[0].Score != 6 || x.Changed[0].Revision != 4 {
		t.Fatalf("changed=%+v", x.Changed)
	}
	got, ok, e := b.Get("a")
	if e != nil || !ok || got.Score != 6 || got.Revision != 4 {
		t.Fatalf("got=%+v ok=%v e=%v", got, ok, e)
	}
}

func TestDeleteThenRecreate(t *testing.T) {
	b := board(t)
	if _, e := b.Apply(Batch{Ops: []Op{up("a", 3)}}); e != nil {
		t.Fatal(e)
	}
	x, e := b.Apply(Batch{Ops: []Op{del("a"), up("a", 9), inc("a", 1)}})
	if e != nil {
		t.Fatal(e)
	}
	// Delete allocates no revision: upsert=2, increment=3.
	if x.Revision != 3 {
		t.Fatalf("x=%+v", x)
	}
	got, ok, _ := b.Get("a")
	if !ok || got.Score != 10 || got.Revision != 3 {
		t.Fatalf("got=%+v", got)
	}
	// Delete of a member deleted earlier in the same batch fails and rolls back.
	before := b.Snapshot()
	if _, e = b.Apply(Batch{Ops: []Op{del("a"), del("a")}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, b.Snapshot()) {
		t.Fatal("state changed after rollback")
	}
}

func TestRevisionNotConsumedOnRollback(t *testing.T) {
	b := board(t)
	if _, e := b.Apply(Batch{Ops: []Op{up("a", 1)}}); e != nil {
		t.Fatal(e)
	}
	// Batch allocates revisions for two upserts, then fails on missing increment.
	if _, e := b.Apply(Batch{Ops: []Op{up("b", 1), up("c", 1), inc("ghost", 1)}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	s := b.Snapshot()
	if s.NextRevision != 2 || s.Generation != 1 || len(s.Entries) != 1 {
		t.Fatalf("s=%+v", s)
	}
	// Next successful batch must reuse the unconsumed revision 2.
	x, e := b.Apply(Batch{Ops: []Op{up("b", 2)}})
	if e != nil || x.Revision != 2 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
	// Overflow and capacity failures also must not consume revisions.
	if _, e = b.Apply(Batch{Ops: []Op{up("c", math.MaxInt64), inc("c", 1)}}); !errors.Is(e, ErrOverflow) {
		t.Fatal(e)
	}
	if b.Snapshot().NextRevision != 3 {
		t.Fatalf("s=%+v", b.Snapshot())
	}
}

func TestExtremeArithmetic(t *testing.T) {
	b := board(t)
	if _, e := b.Apply(Batch{Ops: []Op{
		up("max", math.MaxInt64), up("min", math.MinInt64),
	}}); e != nil {
		t.Fatal(e)
	}
	if _, e := b.Apply(Batch{Ops: []Op{inc("max", 1)}}); !errors.Is(e, ErrOverflow) {
		t.Fatalf("e=%v", e)
	}
	if _, e := b.Apply(Batch{Ops: []Op{inc("min", -1)}}); !errors.Is(e, ErrOverflow) {
		t.Fatalf("e=%v", e)
	}
	// Boundary-exact additions are allowed.
	if _, e := b.Apply(Batch{Ops: []Op{inc("max", 0+0)}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal("zero delta must be rejected")
	}
	if _, e := b.Apply(Batch{Ops: []Op{inc("min", math.MaxInt64)}}); e != nil {
		t.Fatal(e)
	}
	got, _, _ := b.Get("min")
	if got.Score != -1 {
		t.Fatalf("got=%+v", got)
	}
	if _, e := b.Apply(Batch{Ops: []Op{inc("min", math.MinInt64+1)}}); e != nil {
		t.Fatal(e)
	}
	got, _, _ = b.Get("min")
	if got.Score != math.MinInt64 {
		t.Fatalf("got=%+v", got)
	}
	// Upsert may set extreme scores directly.
	if _, e := b.Apply(Batch{Ops: []Op{up("max", math.MinInt64)}}); e != nil {
		t.Fatal(e)
	}
}

func TestFinalCapacityCheckedAfterAllOps(t *testing.T) {
	small, e := New(Options{MaxMembers: 2, MaxNameBytes: 8})
	if e != nil {
		t.Fatal(e)
	}
	// Net count stays within capacity even though intermediate count exceeds it.
	if _, e = small.Apply(Batch{Ops: []Op{
		up("a", 1), up("b", 2), up("c", 3), del("a"),
	}}); e != nil {
		t.Fatal(e)
	}
	if n := len(small.Snapshot().Entries); n != 2 {
		t.Fatalf("n=%d", n)
	}
	// Exceeding final capacity rolls back everything, including the delete.
	before := small.Snapshot()
	if _, e = small.Apply(Batch{Ops: []Op{del("b"), up("d", 4), up("e", 5)}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, small.Snapshot()) {
		t.Fatal("state changed after capacity rollback")
	}
	// Replacing an existing member never consumes capacity.
	full, _ := New(Options{MaxMembers: 1, MaxNameBytes: 8})
	if _, e = full.Apply(Batch{Ops: []Op{up("a", 1), up("a", 2), up("a", 3)}}); e != nil {
		t.Fatal(e)
	}
}

func TestRangeCursorBoundaries(t *testing.T) {
	b := board(t)
	ops := []Op{}
	for i := 0; i < 10; i++ {
		ops = append(ops, up(fmt.Sprintf("m%d", i), int64(i)))
	}
	if _, e := b.Apply(Batch{Ops: ops}); e != nil {
		t.Fatal(e)
	}
	// Page through everything with cursors; no duplicates, no gaps.
	seen := []Entry{}
	cur := Cursor{}
	for {
		page, e := b.Range(0, 9, cur, 3)
		if e != nil {
			t.Fatal(e)
		}
		if len(page) == 0 {
			break
		}
		seen = append(seen, page...)
		last := page[len(page)-1]
		cur = Cursor{Set: true, Score: last.Score, Member: last.Member}
	}
	if len(seen) != 10 {
		t.Fatalf("seen=%d", len(seen))
	}
	for i, en := range seen {
		want := fmt.Sprintf("m%d", 9-i)
		if en.Member != want {
			t.Fatalf("seen[%d]=%s want %s", i, en.Member, want)
		}
	}
	// Cursor on a non-existent member still pages correctly.
	x, e := b.Range(0, 9, Cursor{Set: true, Score: 8, Member: "zzz"}, 100)
	if e != nil || len(x) != 8 || x[0].Member != "m7" {
		t.Fatalf("x=%+v e=%v", x, e)
	}
	// Cursor past the end yields nothing.
	x, e = b.Range(0, 9, Cursor{Set: true, Score: 0, Member: "m0"}, 100)
	if e != nil || len(x) != 0 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
	// Limit boundaries.
	if _, e = b.Range(0, 9, Cursor{}, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e = b.Range(0, 9, Cursor{}, 1001); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e = b.Range(0, 9, Cursor{}, 1000); e != nil {
		t.Fatal(e)
	}
	// Equal min/max selects exactly one score.
	x, e = b.Range(5, 5, Cursor{}, 10)
	if e != nil || len(x) != 1 || x[0].Member != "m5" {
		t.Fatalf("x=%+v e=%v", x, e)
	}
}

func TestSortingTieBreak(t *testing.T) {
	b := board(t)
	if _, e := b.Apply(Batch{Ops: []Op{
		up("b", 5), up("a", 5), up("c", 5), up("z", 6), up("y", -4),
	}}); e != nil {
		t.Fatal(e)
	}
	s := b.Snapshot()
	want := []string{"z", "a", "b", "c", "y"}
	if len(s.Entries) != len(want) {
		t.Fatalf("s=%+v", s)
	}
	for i, w := range want {
		if s.Entries[i].Member != w {
			t.Fatalf("entries[%d]=%s want %s", i, s.Entries[i].Member, w)
		}
	}
	// Changed is sorted the same way.
	x, e := b.Apply(Batch{Ops: []Op{inc("c", 1), inc("a", 1)}})
	if e != nil {
		t.Fatal(e)
	}
	if len(x.Changed) != 2 || x.Changed[0].Member != "a" || x.Changed[1].Member != "c" {
		t.Fatalf("changed=%+v", x.Changed)
	}
}

func TestOwnershipIsolation(t *testing.T) {
	b := board(t)
	if _, e := b.Apply(Batch{Ops: []Op{up("a", 1)}}); e != nil {
		t.Fatal(e)
	}
	s1 := b.Snapshot()
	s1.Entries[0].Score = 999
	r1, _ := b.Range(math.MinInt64, math.MaxInt64, Cursor{}, 10)
	r1[0].Member = "hacked"
	s2 := b.Snapshot()
	if s2.Entries[0].Score != 1 || s2.Entries[0].Member != "a" {
		t.Fatalf("s2=%+v", s2)
	}
}

func TestConcurrentMixed(t *testing.T) {
	b, e := New(Options{MaxMembers: 256, MaxNameBytes: 16})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.Apply(Batch{Ops: []Op{up("shared", 0)}}); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			m := fmt.Sprintf("w-%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = b.Apply(Batch{Ops: []Op{inc("shared", 1)}})
				_, _ = b.Apply(Batch{Ops: []Op{up(m, int64(j))}})
				_, _, _ = b.Get(m)
				_, _ = b.Range(0, 1000, Cursor{}, 5)
				_ = b.Snapshot()
			}
		}()
	}
	wg.Wait()
	got, ok, e := b.Get("shared")
	if e != nil || !ok || got.Score != 32*20 {
		t.Fatalf("got=%+v ok=%v e=%v", got, ok, e)
	}
	s := b.Snapshot()
	if len(s.Entries) != 33 {
		t.Fatalf("s=%+v", len(s.Entries))
	}
	// Every increment and upsert allocated exactly one revision.
	if s.NextRevision != 1+32*20*2+1 {
		t.Fatalf("next=%d", s.NextRevision)
	}
}
