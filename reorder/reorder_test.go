package reorder

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sync"
	"testing"
)

func TestBatchRollbackOnLateConflict(t *testing.T) {
	b := newBuffer(t, opts())
	_, _ = b.PushBatch([]Event{{Stream: "s", Sequence: 5, Payload: []byte("keep")}})
	before := b.Snapshot()
	// Earlier events in the batch succeed; a later conflict must roll back all.
	_, err := b.PushBatch([]Event{
		{Stream: "s", Sequence: 1, Payload: []byte("one")},
		{Stream: "s", Sequence: 2, Payload: []byte("two")},
		{Stream: "s", Sequence: 5, Payload: []byte("other")},
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err=%v", err)
	}
	if !reflect.DeepEqual(before, b.Snapshot()) {
		t.Fatal("late conflict did not roll back earlier events")
	}
}

func TestBatchRollbackOnCapacityMidChain(t *testing.T) {
	b := newBuffer(t, Options{MaxStreams: 2, MaxBuffered: 2, MaxPayloadBytes: 64, MaxStreamBytes: 8})
	before := b.Snapshot()
	// Final candidate would buffer 3 events > MaxBuffered=2.
	_, err := b.PushBatch([]Event{
		{Stream: "a", Sequence: 2, Payload: []byte("a2")},
		{Stream: "b", Sequence: 2, Payload: []byte("b2")},
		{Stream: "b", Sequence: 3, Payload: []byte("b3")},
	})
	if !errors.Is(err, ErrCapacity) {
		t.Fatalf("err=%v", err)
	}
	if !reflect.DeepEqual(before, b.Snapshot()) {
		t.Fatal("capacity failure mutated state")
	}
	// Payload byte capacity also enforced on final state.
	_, err = b.PushBatch([]Event{
		{Stream: "a", Sequence: 2, Payload: bytes.Repeat([]byte("x"), 40)},
		{Stream: "b", Sequence: 2, Payload: bytes.Repeat([]byte("y"), 40)},
	})
	if !errors.Is(err, ErrCapacity) {
		t.Fatalf("bytes err=%v", err)
	}
}

func TestSkipBoundaries(t *testing.T) {
	b := newBuffer(t, opts())
	if _, err := b.Skip("s", 0); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("zero=%v", err)
	}
	if _, err := b.Skip("s", math.MaxUint64); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("max=%v", err)
	}
	if _, err := b.Skip("", 1); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("empty stream=%v", err)
	}
	if _, err := b.Skip("missing", 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing=%v", err)
	}
	// Skip to the maximum valid sequence; Next becomes MaxUint64.
	if _, err := b.PushBatch([]Event{{Stream: "s", Sequence: 3, Payload: []byte("three")}}); err != nil {
		t.Fatal(err)
	}
	ready, err := b.Skip("s", math.MaxUint64-1)
	if err != nil || len(ready) != 0 {
		t.Fatalf("ready=%v err=%v", ready, err)
	}
	if got := b.Snapshot().State[0].Next; got != math.MaxUint64 {
		t.Fatalf("next=%d", got)
	}
	// Buffered event beyond old Next was discarded by the skip.
	if n := b.Snapshot().Buffered; n != 0 {
		t.Fatalf("buffered=%d", n)
	}
}

func TestPushMaxSequenceBoundary(t *testing.T) {
	b := newBuffer(t, opts())
	if _, err := b.PushBatch([]Event{{Stream: "s", Sequence: 0, Payload: []byte("x")}}); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("zero=%v", err)
	}
	if _, err := b.PushBatch([]Event{{Stream: "s", Sequence: math.MaxUint64, Payload: []byte("x")}}); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("max=%v", err)
	}
	if _, err := b.PushBatch([]Event{{Stream: "s", Sequence: math.MaxUint64 - 1, Payload: []byte("x")}}); err != nil {
		t.Fatalf("max-1=%v", err)
	}
}

func TestDeleteNotEmptyAndGeneration(t *testing.T) {
	b := newBuffer(t, opts())
	_, _ = b.PushBatch([]Event{{Stream: "s", Sequence: 2, Payload: []byte("x")}})
	if err := b.Delete("s"); !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("notempty=%v", err)
	}
	if err := b.Delete(string(make([]byte, 9))); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("long name=%v", err)
	}
	gen := b.Snapshot().Generation
	if _, err := b.Skip("s", 2); err != nil { // releases buffered seq 2
		t.Fatal(err)
	}
	if err := b.Delete("s"); err != nil {
		t.Fatal(err)
	}
	s := b.Snapshot()
	if s.Generation != gen+2 || s.Streams != 0 {
		t.Fatalf("snapshot=%+v", s)
	}
}

func TestOwnershipIsolation(t *testing.T) {
	b := newBuffer(t, opts())
	stored := []byte("buf")
	if _, err := b.PushBatch([]Event{{Stream: "s", Sequence: 2, Payload: stored}}); err != nil {
		t.Fatal(err)
	}
	stored[0] = 'X' // mutating input must not affect stored copy

	rel := []byte("rel")
	ready, err := b.PushBatch([]Event{{Stream: "s", Sequence: 1, Payload: rel}})
	if err != nil || len(ready) != 2 {
		t.Fatalf("ready=%v err=%v", ready, err)
	}
	rel[0] = 'Y' // mutating input must not affect returned copy
	if string(ready[0].Payload) != "rel" || string(ready[1].Payload) != "buf" {
		t.Fatalf("ready=%q,%q", ready[0].Payload, ready[1].Payload)
	}
	// Mutating returned payloads must not affect internal state or snapshots.
	ready[1].Payload[0] = 'Z'
	if got := b.Snapshot().State[0].Buffered; len(got) != 0 {
		t.Fatalf("buffered=%v", got)
	}
	// Re-buffer and check snapshot isolation both ways.
	in := []byte("abc")
	_, _ = b.PushBatch([]Event{{Stream: "s", Sequence: 5, Payload: in}})
	in[0] = 'Q'
	snap := b.Snapshot()
	if string(snap.State[0].Buffered[0].Payload) != "abc" {
		t.Fatalf("snap=%q", snap.State[0].Buffered[0].Payload)
	}
	snap.State[0].Buffered[0].Payload[0] = 'W'
	if got := b.Snapshot().State[0].Buffered[0].Payload; string(got) != "abc" {
		t.Fatalf("alias=%q", got)
	}
}

func TestEmptyBatchAndGenerationNoop(t *testing.T) {
	b := newBuffer(t, opts())
	if out, err := b.PushBatch(nil); err != nil || len(out) != 0 {
		t.Fatalf("out=%v err=%v", out, err)
	}
	if g := b.Snapshot().Generation; g != 0 {
		t.Fatalf("gen=%d", g)
	}
	// Idempotent duplicate-only batch does not advance generation.
	_, _ = b.PushBatch([]Event{{Stream: "s", Sequence: 3, Payload: []byte("x")}})
	g := b.Snapshot().Generation
	if out, err := b.PushBatch([]Event{{Stream: "s", Sequence: 3, Payload: []byte("x")}}); err != nil || len(out) != 0 {
		t.Fatalf("dup out=%v err=%v", out, err)
	}
	if got := b.Snapshot().Generation; got != g {
		t.Fatalf("gen %d != %d", got, g)
	}
}

func TestConcurrentMixed(t *testing.T) {
	b := newBuffer(t, Options{MaxStreams: 8, MaxBuffered: 4096, MaxPayloadBytes: 1 << 20, MaxStreamBytes: 16})
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		w := w
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("stream-%d", w)
			for i := uint64(1); i <= 200; i++ {
				if _, err := b.PushBatch([]Event{{Stream: name, Sequence: i, Payload: []byte{byte(i)}}}); err != nil {
					t.Error(err)
					return
				}
				if i%50 == 0 {
					_ = b.Snapshot()
				}
			}
		}()
	}
	wg.Wait()
	s := b.Snapshot()
	if s.Streams != 4 || s.Buffered != 0 {
		t.Fatalf("snapshot=%+v", s)
	}
	for _, st := range s.State {
		if st.Next != 201 {
			t.Fatalf("next=%d", st.Next)
		}
	}
}
