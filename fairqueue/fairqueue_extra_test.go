package fairqueue

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestDeleteReaddFreshSequenceAndFIFO(t *testing.T) {
	q := newQueue(t)
	if _, err := q.ApplyBatch([]Change{put("x", "a", []byte("1")), put("y", "a", []byte("2"))}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.ApplyBatch([]Change{del("x"), put("x", "a", []byte("3"))}); err != nil {
		t.Fatal(err)
	}
	s := q.Snapshot()
	if ids(s.Items) != "y,x" || s.Items[1].Sequence != 3 {
		t.Fatalf("items=%+v", s.Items)
	}
	out, err := q.Dequeue(2)
	if err != nil || ids(out.Tasks) != "y,x" {
		t.Fatalf("fifo=%+v %v", out, err)
	}
}

func TestRollbackDoesNotConsumeSequence(t *testing.T) {
	q := newQueue(t)
	if _, err := q.ApplyBatch([]Change{put("a", "a", []byte{})}); err != nil {
		t.Fatal(err)
	}
	before := q.Snapshot()
	// Fails on existence semantics after a successful candidate Put.
	if _, err := q.ApplyBatch([]Change{put("b", "a", []byte{}), del("ghost")}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
	// Fails on final capacity after candidate Puts.
	big := bytes.Repeat([]byte("z"), 64)
	if _, err := q.ApplyBatch([]Change{put("c", "a", big), put("d", "a", big)}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("err=%v", err)
	}
	after := q.Snapshot()
	if !reflect.DeepEqual(before, after) || after.NextSequence != 2 {
		t.Fatalf("rollback leaked: %+v", after)
	}
	if _, err := q.ApplyBatch([]Change{put("b", "a", []byte{})}); err != nil {
		t.Fatal(err)
	}
	if s := q.Snapshot(); s.Items[1].ID != "b" || s.Items[1].Sequence != 2 {
		t.Fatalf("sequence=%+v", s.Items)
	}
}

func TestEmptyQueueSkipAndCursorStability(t *testing.T) {
	q, err := New(Options{MaxTasks: 8, MaxPayloadBytes: 64, MaxNameBytes: 8},
		[]QueueWeight{{Queue: "empty", Weight: 3}, {Queue: "full", Weight: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.ApplyBatch([]Change{put("f1", "full", []byte{})}); err != nil {
		t.Fatal(err)
	}
	out, err := q.Dequeue(1)
	if err != nil || ids(out.Tasks) != "f1" || out.Cursor != 0 {
		t.Fatalf("out=%+v", out)
	}
	// Peek does not move the stored cursor.
	if _, err := q.ApplyBatch([]Change{put("f2", "full", []byte{})}); err != nil {
		t.Fatal(err)
	}
	p, err := q.Peek(1)
	if err != nil || p.Cursor != 0 || q.Snapshot().Cursor != 0 {
		t.Fatalf("peek=%+v", p)
	}
}

func TestWeightedFairnessRatio(t *testing.T) {
	q, err := New(Options{MaxTasks: 64, MaxPayloadBytes: 1024, MaxNameBytes: 8},
		[]QueueWeight{{Queue: "w3", Weight: 3}, {Queue: "w1", Weight: 1}})
	if err != nil {
		t.Fatal(err)
	}
	var batch []Change
	for i := 0; i < 6; i++ {
		batch = append(batch, put(fmt.Sprintf("a%d", i), "w3", []byte{}))
		batch = append(batch, put(fmt.Sprintf("b%d", i), "w1", []byte{}))
	}
	if _, err := q.ApplyBatch(batch); err != nil {
		t.Fatal(err)
	}
	out, err := q.Dequeue(8)
	if err != nil {
		t.Fatal(err)
	}
	// Wheel [w1,w3,w3,w3]: first 8 picks interleave 2:6.
	want := "b0,a0,a1,a2,b1,a3,a4,a5"
	if ids(out.Tasks) != want {
		t.Fatalf("got %s want %s", ids(out.Tasks), want)
	}
}

func TestLimitValidation(t *testing.T) {
	q := newQueue(t)
	for _, limit := range []int{0, -1, 1001} {
		if _, err := q.Peek(limit); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("peek(%d)=%v", limit, err)
		}
		if _, err := q.Dequeue(limit); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("dequeue(%d)=%v", limit, err)
		}
	}
}

func TestCapacityEdgeAndEmptyPayload(t *testing.T) {
	q, err := New(Options{MaxTasks: 2, MaxPayloadBytes: 4, MaxNameBytes: 8}, []QueueWeight{{Queue: "q", Weight: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.ApplyBatch([]Change{put("a", "q", []byte("ab")), put("b", "q", []byte("cd"))}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.ApplyBatch([]Change{put("c", "q", []byte{})}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("maxTasks=%v", err)
	}
	if _, err := q.ApplyBatch([]Change{del("a"), put("c", "q", []byte("ef"))}); err != nil {
		t.Fatalf("swap=%v", err)
	}
	if s := q.Snapshot(); s.PayloadBytes != 4 || s.Tasks != 2 {
		t.Fatalf("snapshot=%+v", s)
	}
}

func TestPayloadOwnershipBothDirections(t *testing.T) {
	q, _ := New(Options{MaxTasks: 4, MaxPayloadBytes: 16, MaxNameBytes: 8}, []QueueWeight{{Queue: "q", Weight: 1}})
	in := []byte("data")
	if _, err := q.ApplyBatch([]Change{put("a", "q", in)}); err != nil {
		t.Fatal(err)
	}
	in[0] = 'X'
	p, _ := q.Peek(1)
	if string(p.Tasks[0].Payload) != "data" {
		t.Fatalf("input alias: %q", p.Tasks[0].Payload)
	}
	p.Tasks[0].Payload[0] = 'Y'
	d, _ := q.Dequeue(1)
	if string(d.Tasks[0].Payload) != "data" {
		t.Fatalf("peek alias: %q", d.Tasks[0].Payload)
	}
	d.Tasks[0].Payload[0] = 'Z'
	if _, err := q.ApplyBatch([]Change{put("b", "q", []byte("keep"))}); err != nil {
		t.Fatal(err)
	}
	if s := q.Snapshot(); string(s.Items[0].Payload) != "keep" {
		t.Fatalf("snapshot=%q", s.Items[0].Payload)
	}
}

func TestConcurrentMixedOperations(t *testing.T) {
	q, _ := New(Options{MaxTasks: 256, MaxPayloadBytes: 4096, MaxNameBytes: 16}, weights())
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				id := fmt.Sprintf("id-%d-%d", i, j)
				queue := "a"
				if j%2 == 0 {
					queue = "b"
				}
				_, _ = q.ApplyBatch([]Change{put(id, queue, []byte{byte(j)})})
				_, _ = q.Peek(10)
				_, _ = q.Dequeue(3)
				_ = q.Snapshot()
				_, _ = q.ApplyBatch([]Change{del(id)})
			}
		}()
	}
	wg.Wait()
	s := q.Snapshot()
	if s.Tasks < 0 || s.PayloadBytes < 0 || s.Tasks != len(s.Items) {
		t.Fatalf("inconsistent snapshot=%+v", s)
	}
	seen := make(map[string]bool)
	for _, task := range s.Items {
		if seen[task.ID] {
			t.Fatalf("duplicate id %s", task.ID)
		}
		seen[task.ID] = true
	}
}
