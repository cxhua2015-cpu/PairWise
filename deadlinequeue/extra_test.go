package deadlinequeue

import (
	"bytes"
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
)

func TestDeleteThenReaddSameBatchSemantics(t *testing.T) {
	q := newQueue(t, options())
	_, _ = q.ApplyBatch([]Change{add("x", "q", 1, 0, []byte("v1"))})
	if _, err := q.ApplyBatch([]Change{add("x", "q", 9, 9, []byte("v2"))}); !errors.Is(err, ErrExists) {
		t.Fatalf("dup add=%v", err)
	}
	gen, err := q.ApplyBatch([]Change{del("x"), add("x", "q2", 5, -3, []byte("v3"))})
	if err != nil || gen != 2 {
		t.Fatalf("gen=%d err=%v", gen, err)
	}
	s := q.Snapshot()
	if s.Tasks != 1 || s.Items[0].Queue != "q2" || s.Items[0].Due != 5 || s.Items[0].Priority != -3 || string(s.Items[0].Payload) != "v3" {
		t.Fatalf("snapshot=%+v", s)
	}
	// add-then-delete of same ID in one batch nets to removal.
	if _, err := q.ApplyBatch([]Change{del("x"), add("x", "q", 0, 0, []byte{}), del("x")}); err != nil {
		t.Fatal(err)
	}
	if got := q.Snapshot().Tasks; got != 0 {
		t.Fatalf("tasks=%d", got)
	}
}

func TestRollbackOnCapacityAndStructuralPriority(t *testing.T) {
	q := newQueue(t, Options{MaxTasks: 2, MaxPayloadBytes: 4, MaxNameBytes: 8})
	_, _ = q.ApplyBatch([]Change{add("a", "q", 0, 0, []byte("ab"))})
	before := q.Snapshot()
	// Structural error later in batch must win over earlier capacity/exists issues.
	_, err := q.ApplyBatch([]Change{add("b", "q", 0, 0, []byte("cd")), add("c", "q", 0, 0, []byte("ef")), {Kind: Delete, Task: Task{ID: "a", Due: 1}}})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err=%v", err)
	}
	// Capacity exceeded on final candidate.
	_, err = q.ApplyBatch([]Change{add("b", "q", 0, 0, []byte("cd")), add("c", "q", 0, 0, []byte("ef"))})
	if !errors.Is(err, ErrCapacity) {
		t.Fatalf("err=%v", err)
	}
	// Payload bytes capacity.
	_, err = q.ApplyBatch([]Change{add("b", "q", 0, 0, []byte("cde"))})
	if !errors.Is(err, ErrCapacity) {
		t.Fatalf("err=%v", err)
	}
	if !reflect.DeepEqual(before, q.Snapshot()) {
		t.Fatal("rollback mutated state")
	}
	// Empty batch returns current generation without advancing it.
	gen, err := q.ApplyBatch(nil)
	if err != nil || gen != before.Generation {
		t.Fatalf("gen=%d err=%v", gen, err)
	}
	if q.Snapshot().Generation != before.Generation {
		t.Fatal("empty batch advanced generation")
	}
}

func TestWindowExtremesAndValidation(t *testing.T) {
	q := newQueue(t, options())
	_, _ = q.ApplyBatch([]Change{
		add("lo", "q", 0, math.MinInt32, []byte{}),
		add("hi", "q", math.MaxInt64, math.MaxInt32, []byte{}),
		add("mid", "q", 7, 0, []byte{}),
	})
	out, err := q.Window("q", 0, math.MaxInt64, 1000)
	if err != nil || len(out) != 2 || out[0].ID != "lo" || out[1].ID != "mid" {
		t.Fatalf("out=%v err=%v", out, err)
	}
	// Half-open: end exclusive.
	out, _ = q.Window("q", 0, 7, 1000)
	if len(out) != 1 || out[0].ID != "lo" {
		t.Fatalf("out=%v", out)
	}
	// Empty window [x,x) is valid.
	if out, err = q.Window("q", 7, 7, 1); err != nil || len(out) != 0 {
		t.Fatalf("out=%v err=%v", out, err)
	}
	for _, tc := range []struct {
		start, end int64
		limit      int
	}{
		{-1, 5, 1}, {6, 5, 1}, {0, 5, 0}, {0, 5, 1001},
	} {
		if _, err := q.Window("q", tc.start, tc.end, tc.limit); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("tc=%+v err=%v", tc, err)
		}
	}
	if _, err := q.Window("", 0, 1, 1); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("queue err=%v", err)
	}
}

func TestPopDueValidationAndUnlimited(t *testing.T) {
	q := newQueue(t, options())
	_, _ = q.ApplyBatch([]Change{add("a", "q", 1, 0, []byte("x")), add("b", "q", 2, 0, []byte("y"))})
	if _, err := q.PopDue("q", -1, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err=%v", err)
	}
	if _, err := q.PopDue("q", 0, 1001); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err=%v", err)
	}
	out, err := q.PopDue("q", 2, 0)
	if err != nil || len(out) != 2 {
		t.Fatalf("out=%v err=%v", out, err)
	}
	s := q.Snapshot()
	if s.Tasks != 0 || s.PayloadBytes != 0 || s.Generation != 2 {
		t.Fatalf("snapshot=%+v", s)
	}
}

func TestOwnershipIsolation(t *testing.T) {
	q := newQueue(t, options())
	in := []byte("abc")
	_, _ = q.ApplyBatch([]Change{add("a", "q", 1, 0, in)})
	in[0] = 'X'
	w, _ := q.Window("q", 0, 10, 10)
	if !bytes.Equal(w[0].Payload, []byte("abc")) {
		t.Fatalf("stored=%q", w[0].Payload)
	}
	w[0].Payload[0] = 'Y'
	p, _ := q.PopDue("q", 1, 1)
	if !bytes.Equal(p[0].Payload, []byte("abc")) {
		t.Fatalf("popped=%q", p[0].Payload)
	}
	p[0].Payload[0] = 'Z'
	_, _ = q.ApplyBatch([]Change{add("b", "q", 1, 0, []byte("ok"))})
	s1 := q.Snapshot()
	s2 := q.Snapshot()
	s1.Items[0].Payload[0] = 'Q'
	if bytes.Equal(s2.Items[0].Payload, s1.Items[0].Payload) && s1.Items[0].Payload[0] == 'Q' {
		t.Fatal("snapshots alias each other")
	}
}

func TestConcurrentMixed(t *testing.T) {
	q := newQueue(t, Options{MaxTasks: 256, MaxPayloadBytes: 4096, MaxNameBytes: 16})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := string(rune('a'+i)) + "x"
			for j := 0; j < 20; j++ {
				var batch []Change
				if j > 0 {
					batch = append(batch, del(id))
				}
				batch = append(batch, add(id, "q", int64(j), int32(j), []byte("p")))
				if _, err := q.ApplyBatch(batch); err != nil {
					t.Errorf("batch=%v", err)
				}
				_, _ = q.PopDue("none", 100, 0)
				_, _ = q.Window("q", 0, math.MaxInt64, 1000)
				_ = q.Snapshot()
			}
		}()
	}
	wg.Wait()
	s := q.Snapshot()
	if s.Tasks != 16 || s.PayloadBytes != 16 {
		t.Fatalf("snapshot=%+v", s)
	}
}
