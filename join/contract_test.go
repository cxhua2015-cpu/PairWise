package join

import (
	"errors"
	"reflect"
	"sort"
	"sync"
	"testing"
)

func ev(side Side, key, id string, at int64, payload string) Update {
	e := Event{Side: side, Key: key, ID: id, Time: at, Payload: []byte(payload)}
	return Update{Event: &e}
}
func wm(side Side, at int64) Update {
	w := Watermark{Side: side, Time: at}
	return Update{Watermark: &w}
}

func mustNew(t *testing.T, o Options) *Joiner {
	t.Helper()
	j, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	return j
}
func baseOptions() Options { return Options{Window: 5, MaxEvents: 20, MaxBytes: 1000} }

func TestNewAndUpdateValidation(t *testing.T) {
	bad := []Options{{}, {Window: -1, MaxEvents: 1, MaxBytes: 1}, {Window: 1, MaxEvents: 0, MaxBytes: 1}, {Window: 1, MaxEvents: 1, MaxBytes: 0}, {Window: 1_000_000_000_001, MaxEvents: 1, MaxBytes: 1}}
	for _, o := range bad {
		if _, err := New(o); !errors.Is(err, ErrInvalid) {
			t.Fatalf("New(%+v)=%v", o, err)
		}
	}
	j := mustNew(t, baseOptions())
	invalid := []Update{{}, {Event: &Event{}, Watermark: &Watermark{Side: Left}}, ev(9, "k", "id", 1, "x"), ev(Left, "", "id", 1, "x"), ev(Left, "k", "", 1, "x"), ev(Left, "k", "id", -1, "x"), wm(9, 1), wm(Left, -1)}
	for _, u := range invalid {
		if got, err := j.Apply(u); !errors.Is(err, ErrInvalid) || !reflect.DeepEqual(got, Outcome{}) {
			t.Fatalf("invalid got=%+v err=%v", got, err)
		}
	}
	if s := j.Snapshot(); s.Count != 0 || s.LeftWatermark != 0 || s.RightWatermark != 0 {
		t.Fatalf("mutation after invalid: %+v", s)
	}
}

func TestOutOfOrderMatchingAndOrdering(t *testing.T) {
	j := mustNew(t, baseOptions())
	for _, u := range []Update{ev(Right, "k", "r2", 13, "R2"), ev(Right, "other", "x", 10, "X"), ev(Right, "k", "r1", 9, "R1")} {
		if _, err := j.Apply(u); err != nil {
			t.Fatal(err)
		}
	}
	o, err := j.Apply(ev(Left, "k", "l1", 10, "L"))
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Matches) != 2 || o.Matches[0].RightID != "r1" || o.Matches[1].RightID != "r2" {
		t.Fatalf("order/matches: %+v", o)
	}
	for _, m := range o.Matches {
		if m.LeftID != "l1" || string(m.Left) != "L" || m.Key != "k" {
			t.Fatalf("orientation: %+v", m)
		}
	}
	dup, err := j.Apply(ev(Left, "k", "l1", 10, "L"))
	if err != nil || len(dup.Matches) != 0 {
		t.Fatalf("duplicate: %+v %v", dup, err)
	}
}

func TestWindowBoundaryAndWatermarkExpiry(t *testing.T) {
	j := mustNew(t, baseOptions())
	if _, err := j.Apply(ev(Left, "k", "l", 10, "L")); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Apply(wm(Right, 15)); err != nil {
		t.Fatal(err)
	}
	if j.Snapshot().Count != 1 {
		t.Fatal("equal expiry boundary removed event")
	}
	o, err := j.Apply(ev(Right, "k", "r", 15, "R"))
	if err != nil || len(o.Matches) != 1 {
		t.Fatalf("boundary match: %+v %v", o, err)
	}
	x, err := j.Apply(wm(Right, 16))
	if err != nil || len(x.Expired) != 1 || x.Expired[0].ID != "l" {
		t.Fatalf("expiry: %+v %v", x, err)
	}
	if _, err = j.Apply(ev(Left, "z", "late", 10, "")); !errors.Is(err, ErrLate) {
		t.Fatalf("late err=%v", err)
	}
}

func TestWatermarkTimeRollbackIsAtomic(t *testing.T) {
	j := mustNew(t, baseOptions())
	if _, err := j.Apply(wm(Left, 7)); err != nil {
		t.Fatal(err)
	}
	before := j.Snapshot()
	got, err := j.Apply(wm(Left, 6))
	if !errors.Is(err, ErrTime) || !reflect.DeepEqual(got, Outcome{}) || !reflect.DeepEqual(before, j.Snapshot()) {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestDuplicateConflictPrecedesLateness(t *testing.T) {
	j := mustNew(t, baseOptions())
	if _, err := j.Apply(ev(Left, "k", "id", 20, "same")); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Apply(wm(Left, 25)); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Apply(ev(Left, "k", "id", 20, "same")); err != nil {
		t.Fatalf("exact duplicate must win: %v", err)
	}
	if _, err := j.Apply(ev(Left, "k", "id", 20, "different")); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflict must win: %v", err)
	}
}

func TestIDScopeAndReuseAfterExpiry(t *testing.T) {
	j := mustNew(t, baseOptions())
	if _, err := j.Apply(ev(Left, "a", "id", 1, "x")); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Apply(ev(Left, "b", "id", 1, "x")); !errors.Is(err, ErrConflict) {
		t.Fatalf("same-side scope: %v", err)
	}
	if _, err := j.Apply(ev(Right, "b", "id", 1, "x")); err != nil {
		t.Fatalf("other side may reuse: %v", err)
	}
	if _, err := j.Apply(wm(Right, 7)); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Apply(ev(Left, "new", "id", 7, "y")); err != nil {
		t.Fatalf("reuse after expiry: %v", err)
	}
}

func TestBatchRollbackDoesNotLosePreparedMatch(t *testing.T) {
	j := mustNew(t, baseOptions())
	if _, err := j.Apply(ev(Right, "k", "r", 10, "R")); err != nil {
		t.Fatal(err)
	}
	before := j.Snapshot()
	out, err := j.ApplyBatch([]Update{ev(Left, "k", "l", 10, "L"), ev(Left, "x", "l", 11, "conflict")})
	if !errors.Is(err, ErrConflict) || out != nil || !reflect.DeepEqual(before, j.Snapshot()) {
		t.Fatalf("rollback out=%v err=%v", out, err)
	}
	o, err := j.Apply(ev(Left, "k", "l", 10, "L"))
	if err != nil || len(o.Matches) != 1 {
		t.Fatalf("retry lost match: %+v %v", o, err)
	}
}

func TestBatchMayRecoverTemporaryCapacity(t *testing.T) {
	j := mustNew(t, Options{Window: 2, MaxEvents: 1, MaxBytes: 100})
	out, err := j.ApplyBatch([]Update{ev(Left, "k", "old", 1, "x"), wm(Right, 4)})
	if err != nil || len(out) != 2 || len(out[1].Expired) != 1 || j.Snapshot().Count != 0 {
		t.Fatalf("batch=%+v err=%v snap=%+v", out, err, j.Snapshot())
	}
	if _, err := j.ApplyBatch([]Update{ev(Left, "k", "a", 10, "x"), ev(Left, "k", "b", 10, "x")}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("capacity=%v", err)
	}
	if j.Snapshot().Count != 0 {
		t.Fatal("capacity failure mutated")
	}
}

func TestEmptyBatchAndCapacityBytes(t *testing.T) {
	j := mustNew(t, Options{Window: 1, MaxEvents: 2, MaxBytes: 4})
	out, err := j.ApplyBatch(nil)
	if err != nil || out == nil || len(out) != 0 {
		t.Fatalf("empty %#v %v", out, err)
	}
	if _, err := j.Apply(ev(Left, "k", "i", 0, "xy")); err != nil {
		t.Fatal(err)
	} // 1+1+2
	if _, err := j.Apply(ev(Right, "k", "j", 0, "")); !errors.Is(err, ErrCapacity) {
		t.Fatalf("bytes=%v", err)
	}
	if s := j.Snapshot(); s.Count != 1 || s.Bytes != 4 {
		t.Fatalf("snap=%+v", s)
	}
}

func TestPayloadOwnershipAndReturnedIsolation(t *testing.T) {
	j := mustNew(t, baseOptions())
	p := []byte("left")
	e := Event{Side: Left, Key: "k", ID: "l", Time: 1, Payload: p}
	if _, err := j.Apply(Update{Event: &e}); err != nil {
		t.Fatal(err)
	}
	p[0] = 'X'
	o, err := j.Apply(ev(Right, "k", "r", 1, "right"))
	if err != nil || string(o.Matches[0].Left) != "left" {
		t.Fatalf("input aliased: %+v %v", o, err)
	}
	o.Matches[0].Left[0] = 'Y'
	o2, err := j.Apply(ev(Right, "k", "r2", 1, "again"))
	if err != nil {
		t.Fatal(err)
	}
	if len(o2.Matches) != 1 || o2.Matches[0].LeftID != "l" || string(o2.Matches[0].Left) != "left" {
		t.Fatalf("return aliased: %+v", o2)
	}
}

func TestSnapshotSortingAndCopy(t *testing.T) {
	j := mustNew(t, baseOptions())
	for _, u := range []Update{ev(Right, "z", "b", 3, ""), ev(Left, "z", "c", 2, "p"), ev(Left, "a", "a", 2, "xx")} {
		if _, err := j.Apply(u); err != nil {
			t.Fatal(err)
		}
	}
	s := j.Snapshot()
	got := []string{}
	for _, e := range s.Events {
		got = append(got, string(rune(e.Side))+e.Key+e.ID)
	}
	want := append([]string(nil), got...)
	sort.Strings(want) // all Left sort before Right; within this fixture lexical agrees with Time/key/id
	if !reflect.DeepEqual(got, want) || s.Count != 3 || s.Bytes != len("zb")+len("zcp")+len("aaxx") {
		t.Fatalf("snapshot=%+v keys=%v", s, got)
	}
	s.Events[0].ID = "mutated"
	if j.Snapshot().Events[0].ID == "mutated" {
		t.Fatal("snapshot slice aliased")
	}
}

func TestConcurrentPublicMethods(t *testing.T) {
	j := mustNew(t, Options{Window: 1000, MaxEvents: 500, MaxBytes: 100000})
	var wg sync.WaitGroup
	for n := 0; n < 100; n++ {
		n := n
		wg.Add(1)
		go func() {
			defer wg.Done()
			side := Left
			if n%2 == 1 {
				side = Right
			}
			_, err := j.Apply(ev(side, "k", string(rune(1000+n)), int64(n), "x"))
			if err != nil {
				t.Errorf("apply: %v", err)
			}
			_ = j.Snapshot()
		}()
	}
	wg.Wait()
	if s := j.Snapshot(); s.Count != 100 {
		t.Fatalf("count=%d", s.Count)
	}
}
