package deadlinequeue

import (
	"bytes"
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
)

func options() Options { return Options{MaxTasks: 8, MaxPayloadBytes: 32, MaxNameBytes: 16} }
func newQueue(t *testing.T, o Options) *Queue {
	t.Helper()
	q, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	return q
}
func add(id, queue string, due int64, priority int32, payload []byte) Change {
	return Change{Kind: Add, Task: Task{ID: id, Queue: queue, Due: due, Priority: priority, Payload: payload}}
}
func del(id string) Change { return Change{Kind: Delete, Task: Task{ID: id}} }

func TestOptionsAndStructuralValidationFirst(t *testing.T) {
	if _, err := New(Options{}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("New=%v", err)
	}
	q := newQueue(t, options())
	_, _ = q.ApplyBatch([]Change{add("x", "q", 1, 0, []byte("x"))})
	before := q.Snapshot()
	_, err := q.ApplyBatch([]Change{add("x", "q", 2, 0, []byte("y")), {Kind: Delete, Task: Task{ID: "x", Queue: "bad"}}})
	if !errors.Is(err, ErrInvalidInput) || !reflect.DeepEqual(before, q.Snapshot()) {
		t.Fatalf("err=%v", err)
	}
	_, err = q.ApplyBatch([]Change{add("bad", "q", -1, 0, []byte{})})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("due=%v", err)
	}
}

func TestDeleteThenReaddAndRollback(t *testing.T) {
	q := newQueue(t, options())
	_, _ = q.ApplyBatch([]Change{add("x", "q", 1, 0, []byte("old"))})
	gen, err := q.ApplyBatch([]Change{del("x"), add("x", "q", 2, 1, []byte("new"))})
	if err != nil || gen != 2 {
		t.Fatalf("gen=%d err=%v", gen, err)
	}
	before := q.Snapshot()
	_, err = q.ApplyBatch([]Change{del("x"), del("missing")})
	if !errors.Is(err, ErrNotFound) || !reflect.DeepEqual(before, q.Snapshot()) {
		t.Fatalf("err=%v", err)
	}
}

func TestFinalCapacityAndOwnership(t *testing.T) {
	q := newQueue(t, Options{MaxTasks: 1, MaxPayloadBytes: 3, MaxNameBytes: 8})
	in := []byte("abc")
	_, _ = q.ApplyBatch([]Change{add("a", "q", 1, 0, in)})
	in[0] = 'X'
	if _, err := q.ApplyBatch([]Change{add("b", "q", 1, 0, []byte("z")), del("a")}); err != nil {
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

func TestPopDueStableOrderLimitAndGeneration(t *testing.T) {
	q := newQueue(t, options())
	_, _ = q.ApplyBatch([]Change{add("c", "q", 2, 9, []byte{}), add("b", "q", 1, 1, []byte{}), add("a", "q", 1, 7, []byte{}), add("x", "other", 0, 0, []byte{})})
	out, err := q.PopDue("q", 2, 2)
	if err != nil || len(out) != 2 || out[0].ID != "a" || out[1].ID != "b" {
		t.Fatalf("out=%v err=%v", out, err)
	}
	before := q.Snapshot()
	out, err = q.PopDue("q", 0, 0)
	if err != nil || len(out) != 0 || !reflect.DeepEqual(before, q.Snapshot()) {
		t.Fatal("empty pop mutated")
	}
}

func TestWindowBoundariesExtremeAndOrder(t *testing.T) {
	q := newQueue(t, options())
	_, _ = q.ApplyBatch([]Change{add("z", "q", math.MaxInt64, 0, []byte{}), add("b", "q", 5, 1, []byte{}), add("a", "q", 5, 2, []byte{})})
	out, err := q.Window("q", 5, math.MaxInt64, 10)
	if err != nil || len(out) != 2 || out[0].ID != "a" || out[1].ID != "b" {
		t.Fatalf("out=%v err=%v", out, err)
	}
	out, err = q.Window("q", math.MaxInt64, math.MaxInt64, 10)
	if err != nil || len(out) != 0 {
		t.Fatalf("empty=%v err=%v", out, err)
	}
}

func TestSnapshotGlobalOrder(t *testing.T) {
	q := newQueue(t, options())
	_, _ = q.ApplyBatch([]Change{add("z", "b", 0, 0, []byte{}), add("b", "a", 1, 1, []byte{}), add("a", "a", 1, 9, []byte{})})
	s := q.Snapshot()
	if len(s.Items) != 3 || s.Items[0].ID != "a" || s.Items[1].ID != "b" || s.Items[2].ID != "z" {
		t.Fatalf("snapshot=%+v", s)
	}
}

func TestConcurrentApplyAndWindow(t *testing.T) {
	q := newQueue(t, Options{MaxTasks: 64, MaxPayloadBytes: 128, MaxNameBytes: 16})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := string(rune('A' + i))
			if _, err := q.ApplyBatch([]Change{add(id, "q", int64(i), int32(i), []byte{id[0]})}); err != nil {
				t.Errorf("add=%v", err)
			}
			_, _ = q.Window("q", 0, 100, 1000)
			_ = q.Snapshot()
		}()
	}
	wg.Wait()
	if s := q.Snapshot(); s.Tasks != 32 || s.PayloadBytes != 32 {
		t.Fatalf("snapshot=%+v", s)
	}
}
