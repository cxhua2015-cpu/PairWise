package quorumlog

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestBoundaryValidation(t *testing.T) {
	if _, err := New(Options{MaxBufferedBytes: 1}); err != nil {
		t.Fatal(err)
	}
	q := mustNew(t, 1<<20)
	long := string(make([]byte, 129))
	if err := q.Open(StreamConfig{ID: long, Voters: []string{"a"}, Quorum: 1}, 0); !errors.Is(err, ErrInvalid) {
		t.Fatalf("long id=%v", err)
	}
	if err := q.Open(StreamConfig{ID: "s", Voters: []string{"a"}, Quorum: 1}, 1_000_000_000_001); !errors.Is(err, ErrInvalid) {
		t.Fatalf("time overflow=%v", err)
	}
	mustOpen(t, q, "s", []string{"a"}, 1, 1_000_000_000_000)
	if _, err := q.Apply(ent("s", 1_000_000_000_001, "d", "x", 1_000_000_000_000)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("index overflow=%v", err)
	}
	if _, err := q.Apply(ent("s", 1_000_000_000_000, "d", "x", 1_000_000_000_000)); err != nil {
		t.Fatalf("max index: %v", err)
	}
	if _, err := q.ExpireBefore(1_000_000_000_001); !errors.Is(err, ErrInvalid) {
		t.Fatalf("cutoff overflow=%v", err)
	}
}

func TestAckMismatchDoesNotBlockQuorum(t *testing.T) {
	q := mustNew(t, 100)
	mustOpen(t, q, "s", []string{"a", "b", "c"}, 2, 0)
	if _, err := q.Apply(ent("s", 1, "d", "x", 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(ack("s", 1, "a", "other", 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(ack("s", 1, "b", "d", 1)); err != nil {
		t.Fatal(err)
	}
	o, err := q.Apply(ack("s", 1, "c", "d", 1))
	if err != nil || len(o.Committed) != 1 {
		t.Fatalf("mismatch blocked quorum: %+v %v", o, err)
	}
}

func TestAckOnCommittedIndex(t *testing.T) {
	q := mustNew(t, 100)
	mustOpen(t, q, "s", []string{"a"}, 1, 0)
	if _, err := q.Apply(ent("s", 1, "d", "x", 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(ack("s", 1, "a", "d", 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(ack("s", 1, "a", "d", 2)); err != nil {
		t.Fatalf("committed ack idempotent: %v", err)
	}
	if _, err := q.Apply(ack("s", 1, "a", "nope", 2)); !errors.Is(err, ErrConflict) {
		t.Fatalf("committed ack conflict=%v", err)
	}
}

func TestSealBelowCommittedPrefix(t *testing.T) {
	q := mustNew(t, 100)
	mustOpen(t, q, "s", []string{"a"}, 1, 0)
	if _, err := q.Apply(ent("s", 1, "d", "x", 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(ack("s", 1, "a", "d", 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(seal("s", 0, 2)); !errors.Is(err, ErrConflict) {
		t.Fatalf("seal below committed=%v", err)
	}
	if _, err := q.Apply(seal("s", 1, 2)); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(seal("s", 2, 3)); !errors.Is(err, ErrConflict) {
		t.Fatalf("second different seal=%v", err)
	}
}

func TestTimeRollbackAtomic(t *testing.T) {
	q := mustNew(t, 100)
	mustOpen(t, q, "s", []string{"a"}, 1, 5)
	if _, err := q.Apply(ent("s", 1, "d", "x", 4)); !errors.Is(err, ErrTime) {
		t.Fatalf("rollback=%v", err)
	}
	if s := q.Snapshot(); s.Streams[0].PendingEntries != 0 || s.Streams[0].LastActivity != 5 {
		t.Fatalf("rollback mutated state: %+v", s.Streams[0])
	}
}

func TestBatchCapacityAcrossStreams(t *testing.T) {
	q := mustNew(t, 5)
	mustOpen(t, q, "a", []string{"r"}, 1, 0)
	mustOpen(t, q, "b", []string{"r"}, 1, 0)
	if _, err := q.ApplyBatch([]Update{
		ent("a", 1, "d", "xxx", 1),
		ent("b", 1, "d", "yyy", 1),
	}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("shared capacity=%v", err)
	}
	if s := q.Snapshot(); s.BufferedBytes != 0 || len(s.Streams) != 2 {
		t.Fatalf("rollback: %+v", s)
	}
	out, err := q.ApplyBatch([]Update{
		ent("a", 1, "d", "xxxxx", 1),
		ack("a", 1, "r", "d", 1),
		ent("b", 1, "d", "yyyyy", 1),
		ack("b", 1, "r", "d", 1),
	})
	if err != nil || len(out) != 4 || q.Snapshot().BufferedBytes != 0 {
		t.Fatalf("temporary overflow across streams: %+v %v", out, err)
	}
}

func TestOutcomePayloadIsolation(t *testing.T) {
	q := mustNew(t, 100)
	mustOpen(t, q, "s", []string{"a"}, 1, 0)
	if _, err := q.Apply(ent("s", 1, "d", "ab", 1)); err != nil {
		t.Fatal(err)
	}
	o1, err := q.Apply(ack("s", 1, "a", "d", 1))
	if err != nil || len(o1.Committed) != 1 {
		t.Fatal(err)
	}
	o1.Committed[0].Payload[0] = 'Z'
	if _, err := q.Apply(ent("s", 1, "d", "ab", 2)); err != nil {
		t.Fatalf("mutated outcome corrupted digest check: %v", err)
	}
}

func TestExpireBoundaryAndSorted(t *testing.T) {
	q := mustNew(t, 100)
	mustOpen(t, q, "b", []string{"a"}, 1, 3)
	mustOpen(t, q, "a", []string{"a"}, 1, 5)
	mustOpen(t, q, "c", []string{"a"}, 1, 7)
	exp, err := q.ExpireBefore(5)
	if err != nil || !reflect.DeepEqual(exp, []ExpiredStream{{Stream: "b", Committed: 0, BufferedBytes: 0}}) {
		t.Fatalf("boundary expiry: %+v %v", exp, err)
	}
	exp, err = q.ExpireBefore(0)
	if err != nil || exp != nil {
		t.Fatalf("no-op expiry: %+v %v", exp, err)
	}
}

func TestConcurrentMixedOperations(t *testing.T) {
	q := mustNew(t, 1_000_000)
	mustOpen(t, q, "s", []string{"a", "b"}, 2, 0)
	var wg sync.WaitGroup
	errCh := make(chan error, 500)
	for i := 1; i <= 50; i++ {
		i := uint64(i)
		for _, f := range []func() error{
			func() error { _, err := q.Apply(ent("s", i, "d", "payload", 1)); return err },
			func() error { _, err := q.Apply(ack("s", i, "a", "d", 1)); return err },
			func() error { _, err := q.Apply(ack("s", i, "b", "d", 1)); return err },
		} {
			wg.Add(1)
			go func() { defer wg.Done(); errCh <- f() }()
		}
	}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = q.Snapshot()
			_, _ = q.ExpireBefore(0)
			_ = q.Open(StreamConfig{ID: "s", Voters: []string{"a", "b"}, Quorum: 2}, 1)
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	s := q.Snapshot()
	if s.Streams[0].Committed != 50 || s.BufferedBytes != 0 {
		t.Fatalf("final: %+v", s)
	}
}

func TestConcurrentBatchesDisjointStreams(t *testing.T) {
	q := mustNew(t, 1_000_000)
	for i := 0; i < 8; i++ {
		mustOpen(t, q, fmt.Sprintf("s%d", i), []string{"a"}, 1, 0)
	}
	var wg sync.WaitGroup
	errCh := make(chan error, 8)
	for i := 0; i < 8; i++ {
		id := fmt.Sprintf("s%d", i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := uint64(1); j <= 20; j++ {
				out, err := q.ApplyBatch([]Update{
					ent(id, j, "d", "x", int64(j)),
					ack(id, j, "a", "d", int64(j)),
				})
				if err != nil {
					errCh <- err
					return
				}
				if len(out) != 2 || len(out[1].Committed) != 1 {
					errCh <- fmt.Errorf("bad outcome %+v", out)
					return
				}
			}
			errCh <- nil
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := q.Snapshot().BufferedBytes; got != 0 {
		t.Fatalf("buffered=%d", got)
	}
}
