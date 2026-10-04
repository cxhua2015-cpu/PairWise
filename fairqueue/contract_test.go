package fairqueue

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func opts() Options          { return Options{MaxTasks: 16, MaxPayloadBytes: 64, MaxNameBytes: 16} }
func weights() []QueueWeight { return []QueueWeight{{Queue: "b", Weight: 1}, {Queue: "a", Weight: 2}} }
func newQueue(t *testing.T) *Queue {
	t.Helper()
	q, err := New(opts(), weights())
	if err != nil {
		t.Fatal(err)
	}
	return q
}
func put(id, queue string, payload []byte) Change {
	return Change{Kind: Put, ID: id, Queue: queue, Payload: payload}
}
func del(id string) Change { return Change{Kind: Delete, ID: id} }

func TestOptionsAndStructuralValidationFirst(t *testing.T) {
	bad := []struct {
		o Options
		w []QueueWeight
	}{{Options{}, weights()}, {opts(), nil}, {opts(), []QueueWeight{{Queue: "a", Weight: 0}}}, {opts(), []QueueWeight{{Queue: "a", Weight: 1}, {Queue: "a", Weight: 2}}}, {opts(), []QueueWeight{{Queue: "bad/x", Weight: 1}}}}
	for _, tc := range bad {
		if _, err := New(tc.o, tc.w); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("New=%v", err)
		}
	}
	q := newQueue(t)
	_, _ = q.ApplyBatch([]Change{put("x", "a", []byte("x"))})
	before := q.Snapshot()
	_, err := q.ApplyBatch([]Change{put("x", "a", []byte("again")), {Kind: Delete, ID: "x", Queue: "a"}})
	if !errors.Is(err, ErrInvalidInput) || !reflect.DeepEqual(before, q.Snapshot()) {
		t.Fatalf("err=%v", err)
	}
	_, err = q.ApplyBatch([]Change{put("bad/id", "a", []byte{})})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("id=%v", err)
	}
	_, err = q.ApplyBatch([]Change{put("z", "missing", []byte{})})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("queue=%v", err)
	}
}

func TestBatchSequenceDeleteReaddAndRollback(t *testing.T) {
	q := newQueue(t)
	g, err := q.ApplyBatch([]Change{put("x", "a", []byte("old")), put("y", "b", []byte("y"))})
	if err != nil || g != 1 {
		t.Fatalf("batch=(%d,%v)", g, err)
	}
	g, err = q.ApplyBatch([]Change{del("x"), put("x", "a", []byte("new"))})
	if err != nil || g != 2 {
		t.Fatalf("replace=(%d,%v)", g, err)
	}
	s := q.Snapshot()
	if s.NextSequence != 4 || s.Items[0].ID != "x" || s.Items[0].Sequence != 3 {
		t.Fatalf("snapshot=%+v", s)
	}
	before := q.Snapshot()
	_, err = q.ApplyBatch([]Change{put("z", "a", []byte{}), del("missing")})
	if !errors.Is(err, ErrNotFound) || !reflect.DeepEqual(before, q.Snapshot()) {
		t.Fatalf("rollback=%v", err)
	}
}

func TestFinalCapacityAndOwnership(t *testing.T) {
	q, _ := New(Options{MaxTasks: 1, MaxPayloadBytes: 3, MaxNameBytes: 8}, []QueueWeight{{Queue: "q", Weight: 1}})
	in := []byte("abc")
	_, _ = q.ApplyBatch([]Change{put("a", "q", in)})
	in[0] = 'X'
	if _, err := q.ApplyBatch([]Change{put("b", "q", []byte("z")), del("a")}); err != nil {
		t.Fatalf("final capacity=%v", err)
	}
	s := q.Snapshot()
	if string(s.Items[0].Payload) != "z" {
		t.Fatalf("snapshot=%+v", s)
	}
	s.Items[0].Payload[0] = 'Y'
	if got := q.Snapshot().Items[0].Payload; !bytes.Equal(got, []byte("z")) {
		t.Fatalf("alias=%q", got)
	}
}

func TestWeightedSchedulePeekAndDequeue(t *testing.T) {
	q := newQueue(t)
	_, _ = q.ApplyBatch([]Change{put("a1", "a", []byte{}), put("a2", "a", []byte{}), put("a3", "a", []byte{}), put("b1", "b", []byte{}), put("b2", "b", []byte{})})
	before := q.Snapshot()
	peek, err := q.Peek(5)
	if err != nil || ids(peek.Tasks) != "a1,a2,b1,a3,b2" || peek.Cursor != 0 {
		t.Fatalf("peek=%+v err=%v", peek, err)
	}
	if !reflect.DeepEqual(before, q.Snapshot()) {
		t.Fatal("Peek mutated state")
	}
	out, err := q.Dequeue(4)
	if err != nil || ids(out.Tasks) != "a1,a2,b1,a3" || out.Generation != 2 || out.Cursor != 1 {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	out, err = q.Dequeue(2)
	if err != nil || ids(out.Tasks) != "b2" || out.Cursor != 0 || out.Generation != 3 {
		t.Fatalf("tail=%+v err=%v", out, err)
	}
}

func TestSkipEmptyQueuesAndNoopGeneration(t *testing.T) {
	q := newQueue(t)
	_, _ = q.ApplyBatch([]Change{put("b1", "b", []byte{})})
	out, err := q.Dequeue(1)
	if err != nil || ids(out.Tasks) != "b1" || out.Cursor != 0 {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	before := q.Snapshot()
	out, err = q.Dequeue(5)
	if err != nil || len(out.Tasks) != 0 || !reflect.DeepEqual(before, q.Snapshot()) {
		t.Fatal("empty dequeue mutated")
	}
}

func TestSnapshotOrderAndIsolation(t *testing.T) {
	q := newQueue(t)
	_, _ = q.ApplyBatch([]Change{put("b1", "b", []byte{}), put("a1", "a", []byte{}), put("a2", "a", []byte{})})
	s := q.Snapshot()
	if ids(s.Items) != "a1,a2,b1" || !reflect.DeepEqual(s.Wheel, []string{"a", "a", "b"}) {
		t.Fatalf("snapshot=%+v", s)
	}
	s.Wheel[0] = "x"
	if q.Snapshot().Wheel[0] != "a" {
		t.Fatal("wheel alias")
	}
}

func TestConcurrentApplyAndPeek(t *testing.T) {
	q, _ := New(Options{MaxTasks: 64, MaxPayloadBytes: 128, MaxNameBytes: 16}, weights())
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := fmt.Sprintf("id-%d", i)
			queue := "a"
			if i%2 == 1 {
				queue = "b"
			}
			if _, err := q.ApplyBatch([]Change{put(id, queue, []byte{byte(i)})}); err != nil {
				t.Errorf("put=%v", err)
			}
			_, _ = q.Peek(100)
			_ = q.Snapshot()
		}()
	}
	wg.Wait()
	if s := q.Snapshot(); s.Tasks != 32 || s.PayloadBytes != 32 || s.NextSequence != 33 {
		t.Fatalf("snapshot=%+v", s)
	}
}

func ids(tasks []Task) string {
	out := ""
	for i, task := range tasks {
		if i > 0 {
			out += ","
		}
		out += task.ID
	}
	return out
}
