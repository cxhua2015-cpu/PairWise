package deadlinequeue

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"
)

func TestDeleteThenReaddSameBatch(t *testing.T) {
	q := newQueue(t, options())
	if _, err := q.ApplyBatch([]Change{add("x", "q", 1, 0, []byte("old"))}); err != nil {
		t.Fatal(err)
	}
	gen, err := q.ApplyBatch([]Change{del("x"), add("x", "q2", 5, 3, []byte("new"))})
	if err != nil || gen != 2 {
		t.Fatalf("gen=%d err=%v", gen, err)
	}
	s := q.Snapshot()
	if s.Tasks != 1 || s.Items[0].Queue != "q2" || string(s.Items[0].Payload) != "new" {
		t.Fatalf("snapshot=%+v", s)
	}
	// Add then Delete of the same ID in one batch nets to removal.
	if _, err := q.ApplyBatch([]Change{add("y", "q", 0, 0, []byte{}), del("y")}); err != nil {
		t.Fatal(err)
	}
	if s := q.Snapshot(); s.Tasks != 1 {
		t.Fatalf("tasks=%d", s.Tasks)
	}
	// Re-add of a still-existing ID without delete fails.
	if _, err := q.ApplyBatch([]Change{add("x", "q", 0, 0, []byte{})}); !errors.Is(err, ErrExists) {
		t.Fatalf("err=%v", err)
	}
}

func TestRollbackOnCapacityAndErrors(t *testing.T) {
	q := newQueue(t, Options{MaxTasks: 2, MaxPayloadBytes: 4, MaxNameBytes: 8})
	if _, err := q.ApplyBatch([]Change{add("a", "q", 1, 0, []byte("aa"))}); err != nil {
		t.Fatal(err)
	}
	before := q.Snapshot()
	// Exceeds MaxTasks only in the final candidate.
	if _, err := q.ApplyBatch([]Change{add("b", "q", 2, 0, []byte("b")), add("c", "q", 3, 0, []byte("c"))}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("err=%v", err)
	}
	// Exceeds payload budget in the final candidate.
	if _, err := q.ApplyBatch([]Change{add("b", "q", 2, 0, []byte("bbbb"))}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("err=%v", err)
	}
	// Unknown kind.
	if _, err := q.ApplyBatch([]Change{{Kind: 99}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err=%v", err)
	}
	// Transient overflow that resolves within the batch is allowed.
	if _, err := q.ApplyBatch([]Change{add("b", "q", 2, 0, []byte("bbbb")), del("a")}); err != nil {
		t.Fatalf("transient capacity: %v", err)
	}
	after := q.Snapshot()
	if after.Generation != before.Generation+1 || after.Tasks != 1 || after.PayloadBytes != 4 {
		t.Fatalf("snapshot=%+v", after)
	}
	// Failed batches left no trace and generation only moved on success.
	if before.Generation != 1 {
		t.Fatalf("generation=%d", before.Generation)
	}
}

func TestEmptyBatchNoop(t *testing.T) {
	q := newQueue(t, options())
	g0 := q.Snapshot().Generation
	g, err := q.ApplyBatch(nil)
	if err != nil || g != g0 {
		t.Fatalf("gen=%d err=%v", g, err)
	}
	if _, err := q.ApplyBatch([]Change{}); err != nil {
		t.Fatal(err)
	}
	if q.Snapshot().Generation != g0 {
		t.Fatal("empty batch advanced generation")
	}
}

func TestWindowExtremesAndValidation(t *testing.T) {
	q := newQueue(t, options())
	if _, err := q.ApplyBatch([]Change{
		add("lo", "q", 0, math.MinInt32, []byte{}),
		add("hi", "q", math.MaxInt64, math.MaxInt32, []byte{}),
		add("mid", "q", 100, 0, []byte{}),
	}); err != nil {
		t.Fatal(err)
	}
	// Full-range window.
	out, err := q.Window("q", 0, math.MaxInt64, 1000)
	if err != nil || len(out) != 2 || out[0].ID != "lo" || out[1].ID != "mid" {
		t.Fatalf("out=%v err=%v", out, err)
	}
	// Half-open: end is exclusive.
	out, err = q.Window("q", 0, 100, 1000)
	if err != nil || len(out) != 1 || out[0].ID != "lo" {
		t.Fatalf("out=%v err=%v", out, err)
	}
	// Empty window start==end succeeds.
	if out, err = q.Window("q", 50, 50, 1); err != nil || len(out) != 0 {
		t.Fatalf("out=%v err=%v", out, err)
	}
	// Invalid inputs.
	for _, w := range [][4]int64{{0, -1, 0, 1}, {0, 2, 1, 1}, {0, 0, 1, 0}, {0, 0, 1, 1001}} {
		if _, err := q.Window("q", w[1], w[2], int(w[3])); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("window %v: %v", w, err)
		}
	}
	if _, err := q.Window("", 0, 1, 1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("empty queue name accepted")
	}
	// Window does not mutate.
	if s := q.Snapshot(); s.Tasks != 3 || s.Generation != 1 {
		t.Fatalf("snapshot=%+v", s)
	}
}

func TestPopDueValidationAndBoundary(t *testing.T) {
	q := newQueue(t, options())
	if _, err := q.ApplyBatch([]Change{add("a", "q", 10, 0, []byte("x"))}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.PopDue("q", -1, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err=%v", err)
	}
	if _, err := q.PopDue("q", 0, 1001); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err=%v", err)
	}
	if _, err := q.PopDue("", 0, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err=%v", err)
	}
	// Due == now is due.
	out, err := q.PopDue("q", 10, 0)
	if err != nil || len(out) != 1 || out[0].ID != "a" {
		t.Fatalf("out=%v err=%v", out, err)
	}
	s := q.Snapshot()
	if s.Tasks != 0 || s.PayloadBytes != 0 || s.Generation != 2 {
		t.Fatalf("snapshot=%+v", s)
	}
	// Pop on empty queue: no generation bump.
	if _, err := q.PopDue("q", 100, 0); err != nil {
		t.Fatal(err)
	}
	if q.Snapshot().Generation != 2 {
		t.Fatal("empty pop advanced generation")
	}
}

func TestPayloadOwnershipIsolation(t *testing.T) {
	q := newQueue(t, options())
	in := []byte("abc")
	if _, err := q.ApplyBatch([]Change{add("a", "q", 1, 0, in)}); err != nil {
		t.Fatal(err)
	}
	in[0] = 'X' // mutating input must not affect stored state
	w, err := q.Window("q", 0, 10, 10)
	if err != nil || !bytes.Equal(w[0].Payload, []byte("abc")) {
		t.Fatalf("window=%q err=%v", w[0].Payload, err)
	}
	w[0].Payload[1] = 'Y' // mutating window output must not affect state
	p, err := q.PopDue("q", 1, 0)
	if err != nil || !bytes.Equal(p[0].Payload, []byte("abc")) {
		t.Fatalf("pop=%q err=%v", p[0].Payload, err)
	}
	p[0].Payload[2] = 'Z'
	s1 := q.Snapshot()
	s2 := q.Snapshot()
	if len(s1.Items) != 0 || len(s2.Items) != 0 {
		t.Fatal("pop did not remove")
	}
	// Two snapshots of the same state must not alias each other.
	if _, err := q.ApplyBatch([]Change{add("b", "q", 1, 0, []byte("keep"))}); err != nil {
		t.Fatal(err)
	}
	s1 = q.Snapshot()
	s2 = q.Snapshot()
	s1.Items[0].Payload[0] = 'X'
	if !bytes.Equal(s2.Items[0].Payload, []byte("keep")) {
		t.Fatal("snapshots alias each other")
	}
}

func TestConcurrentMixedOperations(t *testing.T) {
	q := newQueue(t, Options{MaxTasks: 128, MaxPayloadBytes: 256, MaxNameBytes: 16})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := fmt.Sprintf("id-%02d", i)
			for r := 0; r < 20; r++ {
				_, _ = q.ApplyBatch([]Change{del(id), add(id, "q", int64(r), int32(i), []byte{id[0]})})
				_, _ = q.Window("q", 0, math.MaxInt64, 1000)
				_, _ = q.PopDue("q", int64(r), 1)
				_ = q.Snapshot()
			}
		}()
	}
	wg.Wait()
	s := q.Snapshot()
	if s.Tasks != len(s.Items) {
		t.Fatalf("inconsistent snapshot: %+v", s)
	}
	total := 0
	for _, it := range s.Items {
		total += len(it.Payload)
	}
	if total != s.PayloadBytes {
		t.Fatalf("payload bytes=%d want %d", s.PayloadBytes, total)
	}
}
