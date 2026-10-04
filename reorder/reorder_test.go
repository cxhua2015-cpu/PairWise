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

func TestBatchRollbackOnMidBatchConflict(t *testing.T) {
	b, err := New(Options{MaxStreams: 4, MaxBuffered: 16, MaxPayloadBytes: 64, MaxStreamBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.PushBatch([]Event{{Stream: "s", Sequence: 5, Payload: []byte("five")}}); err != nil {
		t.Fatal(err)
	}
	before := b.Snapshot()
	// First event would buffer seq 2; second conflicts with buffered seq 5.
	_, err = b.PushBatch([]Event{
		{Stream: "s", Sequence: 2, Payload: []byte("two")},
		{Stream: "s", Sequence: 5, Payload: []byte("FIVE")},
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err=%v", err)
	}
	if !reflect.DeepEqual(before, b.Snapshot()) {
		t.Fatal("conflict batch did not roll back")
	}
}

func TestCapacityCheckedAfterSemantics(t *testing.T) {
	// Semantic error must win even when capacity would also be exceeded.
	b, err := New(Options{MaxStreams: 1, MaxBuffered: 1, MaxPayloadBytes: 2, MaxStreamBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.PushBatch([]Event{{Stream: "s", Sequence: 2, Payload: []byte("xx")}}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.PushBatch([]Event{
		{Stream: "s", Sequence: 3, Payload: []byte("yy")},
		{Stream: "s", Sequence: 2, Payload: []byte("conflict")},
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("err=%v", err)
	}
}

func TestSkipBoundaryAndMaxSequence(t *testing.T) {
	b, err := New(Options{MaxStreams: 2, MaxBuffered: 8, MaxPayloadBytes: 64, MaxStreamBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	maxSeq := uint64(math.MaxUint64 - 1)
	if _, err := b.PushBatch([]Event{{Stream: "s", Sequence: maxSeq, Payload: []byte("m")}}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.PushBatch([]Event{{Stream: "s", Sequence: math.MaxUint64, Payload: []byte("x")}}); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("push max=%v", err)
	}
	if _, err := b.PushBatch([]Event{{Stream: "s", Sequence: 0, Payload: []byte("x")}}); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("push zero=%v", err)
	}
	// Skip through maxSeq-1 discards nothing but advances Next.
	ready, err := b.Skip("s", maxSeq-1)
	if err != nil || len(ready) != 1 || ready[0].Sequence != maxSeq {
		t.Fatalf("ready=%v err=%v", ready, err)
	}
	if got := b.Snapshot().State[0].Next; got != maxSeq+1 {
		t.Fatalf("next=%d", got)
	}
	// Skip at the new max boundary: through < Next is a no-op.
	if out, err := b.Skip("s", maxSeq); err != nil || len(out) != 0 {
		t.Fatalf("noop skip=%v err=%v", out, err)
	}
	if _, err := b.Skip("s", 0); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("skip zero=%v", err)
	}
	if _, err := b.Skip("missing", 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("skip missing=%v", err)
	}
	if _, err := b.Skip("", 1); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("skip empty name=%v", err)
	}
}

func TestSkipDiscardsAndReleasesSuffix(t *testing.T) {
	b, err := New(Options{MaxStreams: 2, MaxBuffered: 8, MaxPayloadBytes: 64, MaxStreamBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.PushBatch([]Event{
		{Stream: "s", Sequence: 2, Payload: []byte("two")},
		{Stream: "s", Sequence: 4, Payload: []byte("four")},
		{Stream: "s", Sequence: 5, Payload: []byte("five")},
	}); err != nil {
		t.Fatal(err)
	}
	gen := b.Snapshot().Generation
	// through=3 discards buffered seq 2, sets Next=4, releases 4 and 5.
	ready, err := b.Skip("s", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 2 || ready[0].Sequence != 4 || ready[1].Sequence != 5 {
		t.Fatalf("ready=%v", ready)
	}
	s := b.Snapshot()
	if s.Buffered != 0 || s.PayloadBytes != 0 || s.State[0].Next != 6 || s.Generation != gen+1 {
		t.Fatalf("snapshot=%+v", s)
	}
}

func TestDuplicateConflictWithinSameBatch(t *testing.T) {
	b, err := New(Options{MaxStreams: 2, MaxBuffered: 8, MaxPayloadBytes: 64, MaxStreamBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	// Same batch, identical duplicate of a future sequence: idempotent.
	if _, err := b.PushBatch([]Event{
		{Stream: "s", Sequence: 3, Payload: []byte("x")},
		{Stream: "s", Sequence: 3, Payload: []byte("x")},
	}); err != nil {
		t.Fatal(err)
	}
	if got := b.Snapshot().Buffered; got != 1 {
		t.Fatalf("buffered=%d", got)
	}
	// Same batch, conflicting duplicate: whole batch rolls back.
	before := b.Snapshot()
	if _, err := b.PushBatch([]Event{
		{Stream: "s", Sequence: 4, Payload: []byte("y")},
		{Stream: "s", Sequence: 4, Payload: []byte("z")},
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("err=%v", err)
	}
	if !reflect.DeepEqual(before, b.Snapshot()) {
		t.Fatal("intra-batch conflict did not roll back")
	}
}

func TestOwnershipIsolation(t *testing.T) {
	b, err := New(Options{MaxStreams: 2, MaxBuffered: 8, MaxPayloadBytes: 64, MaxStreamBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	in := []byte("in")
	if _, err := b.PushBatch([]Event{{Stream: "s", Sequence: 2, Payload: in}}); err != nil {
		t.Fatal(err)
	}
	in[0] = 'X' // mutating caller input must not affect stored copy
	s := b.Snapshot()
	if !bytes.Equal(s.State[0].Buffered[0].Payload, []byte("in")) {
		t.Fatalf("stored payload mutated: %q", s.State[0].Buffered[0].Payload)
	}
	s.State[0].Buffered[0].Payload[0] = 'Z' // mutating snapshot must not leak
	if got := b.Snapshot().State[0].Buffered[0].Payload; !bytes.Equal(got, []byte("in")) {
		t.Fatalf("snapshot alias: %q", got)
	}
	ready, err := b.PushBatch([]Event{{Stream: "s", Sequence: 1, Payload: []byte("one")}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ready[1].Payload, []byte("in")) {
		t.Fatalf("released payload: %q", ready[1].Payload)
	}
}

func TestDeleteNotEmptyAndGeneration(t *testing.T) {
	b, err := New(Options{MaxStreams: 2, MaxBuffered: 8, MaxPayloadBytes: 64, MaxStreamBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.PushBatch([]Event{{Stream: "s", Sequence: 2, Payload: []byte("x")}}); err != nil {
		t.Fatal(err)
	}
	if err := b.Delete("s"); !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("delete=%v", err)
	}
	if err := b.Delete(""); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("delete invalid=%v", err)
	}
	gen := b.Snapshot().Generation
	if _, err := b.Skip("s", 1); err != nil { // releases seq 2, empties stream
		t.Fatal(err)
	}
	if err := b.Delete("s"); err != nil {
		t.Fatal(err)
	}
	if got := b.Snapshot(); got.Streams != 0 || got.Generation != gen+2 {
		t.Fatalf("snapshot=%+v", got)
	}
}

func TestStreamNameLengthLimit(t *testing.T) {
	b, err := New(Options{MaxStreams: 2, MaxBuffered: 8, MaxPayloadBytes: 64, MaxStreamBytes: 3})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.PushBatch([]Event{{Stream: "abcd", Sequence: 1, Payload: []byte("x")}}); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("long name=%v", err)
	}
	if _, err := b.PushBatch([]Event{{Stream: "abc", Sequence: 1, Payload: []byte("x")}}); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyPayloadValid(t *testing.T) {
	b, err := New(Options{MaxStreams: 2, MaxBuffered: 8, MaxPayloadBytes: 64, MaxStreamBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	ready, err := b.PushBatch([]Event{{Stream: "s", Sequence: 1, Payload: []byte{}}})
	if err != nil || len(ready) != 1 {
		t.Fatalf("ready=%v err=%v", ready, err)
	}
}

func TestConcurrentMixed(t *testing.T) {
	b, err := New(Options{MaxStreams: 8, MaxBuffered: 512, MaxPayloadBytes: 4096, MaxStreamBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("s%d", g)
			for i := uint64(1); i <= 100; i++ {
				if _, err := b.PushBatch([]Event{{Stream: name, Sequence: i, Payload: []byte{byte(i)}}}); err != nil {
					t.Error(err)
					return
				}
				_ = b.Snapshot()
			}
		}()
	}
	wg.Wait()
	s := b.Snapshot()
	if s.Streams != 4 || s.Buffered != 0 {
		t.Fatalf("snapshot=%+v", s)
	}
	for _, st := range s.State {
		if st.Next != 101 {
			t.Fatalf("stream %s next=%d", st.Stream, st.Next)
		}
	}
}
