package delayedqueue

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func opt() Options {
	return Options{MaxJobs: 4, MaxIDBytes: 16, MaxPayloadBytes: 8, MaxTotalPayloadBytes: 16}
}
func nq(t *testing.T) *Queue {
	t.Helper()
	q, e := New(opt())
	if e != nil {
		t.Fatal(e)
	}
	return q
}
func en(id string, p int, at int64, v string) Op {
	return Op{Kind: Enqueue, ID: id, Priority: p, ReadyAt: at, Payload: []byte(v)}
}
func TestValidationBeforeTimeAndState(t *testing.T) {
	if _, e := New(Options{}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	q := nq(t)
	_, _ = q.Apply(Batch{Now: 5, Ops: []Op{en("a", 1, 5, "x")}})
	before := q.Snapshot()
	_, e := q.Apply(Batch{Now: 4, Ops: []Op{{Kind: Cancel, ID: "bad?"}}})
	if !errors.Is(e, ErrInvalidInput) || !reflect.DeepEqual(before, q.Snapshot()) {
		t.Fatalf("e=%v", e)
	}
}
func TestSequentialRevisionAndRollback(t *testing.T) {
	q := nq(t)
	x, e := q.Apply(Batch{Now: 1, Ops: []Op{en("a", 1, 3, "x"), {Kind: Reschedule, ID: "a", Priority: 4, ReadyAt: 2}}})
	if e != nil || x.Revision != 2 || x.Generation != 1 || len(x.Changed) != 1 || x.Changed[0].Revision != 2 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
	before := q.Snapshot()
	_, e = q.Apply(Batch{Now: 2, Ops: []Op{{Kind: Cancel, ID: "a"}, {Kind: Cancel, ID: "missing"}}})
	if !errors.Is(e, ErrNotFound) || !reflect.DeepEqual(before, q.Snapshot()) {
		t.Fatal(e)
	}
}
func TestCancelThenReuseAndFinalCapacity(t *testing.T) {
	q, _ := New(Options{MaxJobs: 1, MaxIDBytes: 8, MaxPayloadBytes: 4, MaxTotalPayloadBytes: 4})
	_, _ = q.Apply(Batch{Ops: []Op{en("a", 0, 0, "1234")}})
	x, e := q.Apply(Batch{Ops: []Op{{Kind: Cancel, ID: "a"}, en("a", 2, 0, "zz")}})
	if e != nil || x.Revision != 2 || len(q.Snapshot().Jobs) != 1 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
}
func TestPeekTakeOrderingAndBoundary(t *testing.T) {
	q := nq(t)
	_, _ = q.Apply(Batch{Now: 1, Ops: []Op{en("b", 2, 2, "b"), en("a", 2, 2, "a"), en("c", 3, 3, "c")}})
	p, e := q.Peek(2, 3)
	if e != nil || len(p) != 2 || p[0].ID != "a" || p[1].ID != "b" {
		t.Fatalf("p=%+v e=%v", p, e)
	}
	g := q.Snapshot().Generation
	x, e := q.Take(3, 2)
	if e != nil || len(x) != 2 || x[0].ID != "c" || x[1].ID != "a" || q.Snapshot().Generation != g+1 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
}
func TestPayloadOwnership(t *testing.T) {
	q := nq(t)
	v := []byte("abc")
	_, _ = q.Apply(Batch{Ops: []Op{{Kind: Enqueue, ID: "a", ReadyAt: 1, Payload: v}}})
	v[0] = 'z'
	s := q.Snapshot()
	s.Jobs[0].Payload[0] = 'y'
	p, _ := q.Peek(1, 1)
	if string(p[0].Payload) != "abc" {
		t.Fatal(string(p[0].Payload))
	}
}
func TestTimeAndEmptyGeneration(t *testing.T) {
	q := nq(t)
	g := q.Snapshot().Generation
	p, e := q.Peek(5, 1)
	if e != nil || len(p) != 0 || q.Snapshot().Generation != g || q.Snapshot().Now != 5 {
		t.Fatal(e)
	}
	if _, e = q.Take(4, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
}
func TestConcurrentCalls(t *testing.T) {
	q, _ := New(Options{MaxJobs: 64, MaxIDBytes: 16, MaxPayloadBytes: 8, MaxTotalPayloadBytes: 512})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := fmt.Sprintf("j-%02d", i)
			_, _ = q.Apply(Batch{Ops: []Op{en(id, i, 0, "x")}})
			_, _ = q.Peek(0, 64)
			_ = q.Snapshot()
		}()
	}
	wg.Wait()
	if len(q.Snapshot().Jobs) != 32 {
		t.Fatal(len(q.Snapshot().Jobs))
	}
}
