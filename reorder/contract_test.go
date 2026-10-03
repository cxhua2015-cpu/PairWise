package reorder

import (
	"bytes"
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
)

func newBuffer(t *testing.T, o Options) *Buffer {
	t.Helper()
	b, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func opts() Options {
	return Options{MaxStreams: 4, MaxBuffered: 8, MaxPayloadBytes: 32, MaxStreamBytes: 8}
}

func TestOptionsAndStructuralValidationFirst(t *testing.T) {
	if _, err := New(Options{}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("New=%v", err)
	}
	b := newBuffer(t, opts())
	_, _ = b.PushBatch([]Event{{Stream: "s", Sequence: 2, Payload: []byte("x")}})
	before := b.Snapshot()
	_, err := b.PushBatch([]Event{{Stream: "s", Sequence: 2, Payload: []byte("different")}, {Stream: "", Sequence: 0, Payload: nil}})
	if !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("validation order=%v", err)
	}
	if !reflect.DeepEqual(before, b.Snapshot()) {
		t.Fatal("invalid batch mutated state")
	}
}

func TestOutOfOrderReleaseAndOrdering(t *testing.T) {
	b := newBuffer(t, opts())
	in := []byte("three")
	ready, err := b.PushBatch([]Event{{Stream: "a", Sequence: 3, Payload: in}, {Stream: "b", Sequence: 1, Payload: []byte("b1")}, {Stream: "a", Sequence: 2, Payload: []byte("a2")}, {Stream: "a", Sequence: 1, Payload: []byte("a1")}})
	if err != nil {
		t.Fatal(err)
	}
	in[0] = 'X'
	want := []Event{{Stream: "b", Sequence: 1, Payload: []byte("b1")}, {Stream: "a", Sequence: 1, Payload: []byte("a1")}, {Stream: "a", Sequence: 2, Payload: []byte("a2")}, {Stream: "a", Sequence: 3, Payload: []byte("three")}}
	if !reflect.DeepEqual(ready, want) {
		t.Fatalf("ready=%+v", ready)
	}
	s := b.Snapshot()
	if s.Streams != 2 || s.Buffered != 0 || s.PayloadBytes != 0 || s.Generation != 1 {
		t.Fatalf("snapshot=%+v", s)
	}
}

func TestDuplicateConflictOldAndRollback(t *testing.T) {
	b := newBuffer(t, opts())
	_, _ = b.PushBatch([]Event{{Stream: "s", Sequence: 3, Payload: []byte("x")}})
	before := b.Snapshot()
	if out, err := b.PushBatch([]Event{{Stream: "s", Sequence: 3, Payload: []byte("x")}}); err != nil || len(out) != 0 {
		t.Fatalf("idempotent out=%v err=%v", out, err)
	}
	if !reflect.DeepEqual(before, b.Snapshot()) {
		t.Fatal("idempotent duplicate mutated")
	}
	if _, err := b.PushBatch([]Event{{Stream: "s", Sequence: 3, Payload: []byte("y")}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflict=%v", err)
	}
	_, _ = b.PushBatch([]Event{{Stream: "s", Sequence: 1, Payload: []byte("one")}})
	before = b.Snapshot()
	if _, err := b.PushBatch([]Event{{Stream: "s", Sequence: 2, Payload: []byte("two")}, {Stream: "s", Sequence: 1, Payload: []byte("old")}}); !errors.Is(err, ErrOldSequence) {
		t.Fatalf("old=%v", err)
	}
	if !reflect.DeepEqual(before, b.Snapshot()) {
		t.Fatal("semantic failure did not roll back")
	}
}

func TestFinalCapacityAndRollback(t *testing.T) {
	b := newBuffer(t, Options{MaxStreams: 1, MaxBuffered: 1, MaxPayloadBytes: 2, MaxStreamBytes: 8})
	ready, err := b.PushBatch([]Event{{Stream: "s", Sequence: 2, Payload: []byte("xx")}, {Stream: "s", Sequence: 1, Payload: []byte("y")}})
	if err != nil || len(ready) != 2 {
		t.Fatalf("final capacity ready=%v err=%v", ready, err)
	}
	before := b.Snapshot()
	if _, err := b.PushBatch([]Event{{Stream: "z", Sequence: 2, Payload: []byte("x")}}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("capacity=%v", err)
	}
	if !reflect.DeepEqual(before, b.Snapshot()) {
		t.Fatal("capacity failure mutated")
	}
}

func TestSkipDeleteAndBoundaries(t *testing.T) {
	b := newBuffer(t, opts())
	_, _ = b.PushBatch([]Event{{Stream: "s", Sequence: 4, Payload: []byte("four")}, {Stream: "s", Sequence: 3, Payload: []byte("three")}})
	ready, err := b.Skip("s", 2)
	if err != nil || len(ready) != 2 || ready[0].Sequence != 3 || ready[1].Sequence != 4 {
		t.Fatalf("ready=%v err=%v", ready, err)
	}
	before := b.Snapshot()
	if out, err := b.Skip("s", 1); err != nil || len(out) != 0 {
		t.Fatalf("noop=%v err=%v", out, err)
	}
	if !reflect.DeepEqual(before, b.Snapshot()) {
		t.Fatal("skip noop mutated")
	}
	if _, err := b.Skip("s", math.MaxUint64); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("max=%v", err)
	}
	if err := b.Delete("s"); err != nil {
		t.Fatal(err)
	}
	if err := b.Delete("s"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing=%v", err)
	}
}

func TestSnapshotSortAndOwnership(t *testing.T) {
	b := newBuffer(t, opts())
	in := []byte("x")
	_, _ = b.PushBatch([]Event{{Stream: "z", Sequence: 3, Payload: in}, {Stream: "a", Sequence: 2, Payload: []byte("a")}})
	in[0] = 'Y'
	s := b.Snapshot()
	if len(s.State) != 2 || s.State[0].Stream != "a" || s.State[1].Stream != "z" || s.State[1].Buffered[0].Sequence != 3 || string(s.State[1].Buffered[0].Payload) != "x" {
		t.Fatalf("snapshot=%+v", s)
	}
	s.State[1].Buffered[0].Payload[0] = 'Q'
	if got := b.Snapshot().State[1].Buffered[0].Payload; !bytes.Equal(got, []byte("x")) {
		t.Fatalf("alias=%q", got)
	}
}

func TestConcurrentPush(t *testing.T) {
	b := newBuffer(t, Options{MaxStreams: 1, MaxBuffered: 128, MaxPayloadBytes: 512, MaxStreamBytes: 8})
	var wg sync.WaitGroup
	for i := uint64(2); i <= 64; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = b.PushBatch([]Event{{Stream: "s", Sequence: i, Payload: []byte{byte(i)}}})
		}()
	}
	wg.Wait()
	ready, err := b.PushBatch([]Event{{Stream: "s", Sequence: 1, Payload: []byte{1}}})
	if err != nil || len(ready) != 64 {
		t.Fatalf("ready=%d err=%v", len(ready), err)
	}
}
