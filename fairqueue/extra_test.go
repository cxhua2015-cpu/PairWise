package fairqueue

import (
	"bytes"
	"errors"
	"fmt"
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
	if s.NextSequence != 4 || len(s.Items) != 2 ||
		s.Items[0].ID != "y" || s.Items[0].Sequence != 2 ||
		s.Items[1].ID != "x" || s.Items[1].Sequence != 3 {
		t.Fatalf("snapshot=%+v", s)
	}
	out, err := q.Dequeue(2)
	if err != nil || ids(out.Tasks) != "y,x" {
		t.Fatalf("fifo order=%s err=%v", ids(out.Tasks), err)
	}
}

func TestRollbackDoesNotConsumeSequence(t *testing.T) {
	q := newQueue(t)
	if _, err := q.ApplyBatch([]Change{put("a1", "a", []byte{})}); err != nil {
		t.Fatal(err)
	}
	for _, batch := range [][]Change{
		{put("b1", "b", []byte{}), put("a1", "a", []byte{})},                         // ErrExists
		{put("b1", "b", []byte{}), del("ghost")},                                     // ErrNotFound
		{put("b1", "b", bytes.Repeat([]byte("x"), 64)), put("b2", "b", []byte("y"))}, // ErrCapacity on final state
	} {
		if _, err := q.ApplyBatch(batch); err == nil {
			t.Fatalf("batch=%v want error", batch)
		}
	}
	s := q.Snapshot()
	if s.NextSequence != 2 || s.Tasks != 1 || s.Generation != 1 {
		t.Fatalf("snapshot=%+v", s)
	}
	if _, err := q.ApplyBatch([]Change{put("b1", "b", []byte{})}); err != nil {
		t.Fatal(err)
	}
	if got := q.Snapshot().Items[1].Sequence; got != 2 {
		t.Fatalf("sequence=%d", got)
	}
}

func TestEmptyBatchReturnsCurrentGeneration(t *testing.T) {
	q := newQueue(t)
	g, err := q.ApplyBatch(nil)
	if err != nil || g != 0 {
		t.Fatalf("empty=(%d,%v)", g, err)
	}
	if _, err := q.ApplyBatch([]Change{put("a1", "a", []byte{})}); err != nil {
		t.Fatal(err)
	}
	g, err = q.ApplyBatch([]Change{})
	if err != nil || g != 1 {
		t.Fatalf("empty=(%d,%v)", g, err)
	}
}

func TestWeightFairnessAcrossRounds(t *testing.T) {
	q, err := New(Options{MaxTasks: 64, MaxPayloadBytes: 64, MaxNameBytes: 8},
		[]QueueWeight{{Queue: "lo", Weight: 1}, {Queue: "hi", Weight: 3}})
	if err != nil {
		t.Fatal(err)
	}
	var batch []Change
	for i := 0; i < 6; i++ {
		batch = append(batch, put(fmt.Sprintf("a%d", i), "hi", []byte{}))
	}
	for i := 0; i < 2; i++ {
		batch = append(batch, put(fmt.Sprintf("b%d", i), "lo", []byte{}))
	}
	if _, err := q.ApplyBatch(batch); err != nil {
		t.Fatal(err)
	}
	out, err := q.Dequeue(8)
	if err != nil {
		t.Fatal(err)
	}
	// wheel [hi,hi,hi,lo]: 3:1 interleave preserved across rounds.
	want := "a0,a1,a2,b0,a3,a4,a5,b1"
	if ids(out.Tasks) != want {
		t.Fatalf("order=%s want %s", ids(out.Tasks), want)
	}
	if out.Cursor != 0 {
		t.Fatalf("cursor=%d", out.Cursor)
	}
}

func TestCursorPersistsAcrossDequeues(t *testing.T) {
	q := newQueue(t) // wheel [a,a,b]
	if _, err := q.ApplyBatch([]Change{
		put("a1", "a", []byte{}), put("a2", "a", []byte{}),
		put("b1", "b", []byte{}), put("b2", "b", []byte{}),
	}); err != nil {
		t.Fatal(err)
	}
	out, err := q.Dequeue(1) // takes a1, cursor=1
	if err != nil || ids(out.Tasks) != "a1" || out.Cursor != 1 {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	out, err = q.Dequeue(1) // takes a2, cursor=2
	if err != nil || ids(out.Tasks) != "a2" || out.Cursor != 2 {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	out, err = q.Dequeue(1) // takes b1, cursor=0
	if err != nil || ids(out.Tasks) != "b1" || out.Cursor != 0 {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

func TestLimitValidation(t *testing.T) {
	q := newQueue(t)
	for _, limit := range []int{0, -1, 1001} {
		if _, err := q.Peek(limit); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("Peek(%d)=%v", limit, err)
		}
		if _, err := q.Dequeue(limit); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("Dequeue(%d)=%v", limit, err)
		}
	}
}

func TestCapacityTaskCountAndPayloadBytes(t *testing.T) {
	q, err := New(Options{MaxTasks: 2, MaxPayloadBytes: 4, MaxNameBytes: 8},
		[]QueueWeight{{Queue: "q", Weight: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.ApplyBatch([]Change{put("a", "q", []byte("12")), put("b", "q", []byte("34"))}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.ApplyBatch([]Change{put("c", "q", []byte{})}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("count err=%v", err)
	}
	// Delete-then-put succeeds because only the final state is checked.
	if _, err := q.ApplyBatch([]Change{del("a"), put("c", "q", []byte("56"))}); err != nil {
		t.Fatalf("final-state capacity=%v", err)
	}
	if _, err := q.ApplyBatch([]Change{put("d", "q", []byte("7"))}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("bytes err=%v", err)
	}
}

func TestPayloadOwnershipBothDirections(t *testing.T) {
	q := newQueue(t)
	in := []byte("abc")
	if _, err := q.ApplyBatch([]Change{put("x", "a", in)}); err != nil {
		t.Fatal(err)
	}
	in[0] = 'X'
	peek, err := q.Peek(1)
	if err != nil || string(peek.Tasks[0].Payload) != "abc" {
		t.Fatalf("peek=%+v err=%v", peek, err)
	}
	peek.Tasks[0].Payload[0] = 'Y'
	out, err := q.Dequeue(1)
	if err != nil || string(out.Tasks[0].Payload) != "abc" {
		t.Fatalf("dequeue=%+v err=%v", out, err)
	}
	out.Tasks[0].Payload[0] = 'Z'
	if _, err := q.ApplyBatch([]Change{put("y", "a", []byte("q"))}); err != nil {
		t.Fatal(err)
	}
	s := q.Snapshot()
	s.Items[0].Payload[0] = 'W'
	if got := q.Snapshot().Items[0].Payload; string(got) != "q" {
		t.Fatalf("alias=%q", got)
	}
}

func TestConcurrentMixedOperations(t *testing.T) {
	q, _ := New(Options{MaxTasks: 128, MaxPayloadBytes: 256, MaxNameBytes: 16}, weights())
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := fmt.Sprintf("c-%d", i)
			queue := "a"
			if i%2 == 1 {
				queue = "b"
			}
			if _, err := q.ApplyBatch([]Change{put(id, queue, []byte{byte(i)})}); err != nil {
				t.Errorf("put=%v", err)
			}
			_, _ = q.Peek(10)
			_, _ = q.Dequeue(1)
			_ = q.Snapshot()
			_, _ = q.ApplyBatch([]Change{del(id)})
		}()
	}
	wg.Wait()
	s := q.Snapshot()
	if s.Tasks != len(s.Items) || s.Tasks > 16 {
		t.Fatalf("snapshot=%+v", s)
	}
	for _, it := range s.Items {
		if len(it.Payload) != 1 {
			t.Fatalf("payload=%v", it.Payload)
		}
	}
}
