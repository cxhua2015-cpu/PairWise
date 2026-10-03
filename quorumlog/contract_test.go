package quorumlog

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func mustNew(t *testing.T, max int) *Tracker {
	t.Helper()
	q, err := New(Options{MaxBufferedBytes: max})
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func mustOpen(t *testing.T, q *Tracker, id string, voters []string, quorum int, at int64) {
	t.Helper()
	if err := q.Open(StreamConfig{ID: id, Voters: voters, Quorum: quorum}, at); err != nil {
		t.Fatal(err)
	}
}

func ent(stream string, index uint64, digest, payload string, at int64) Update {
	e := Entry{Index: index, Digest: digest, Payload: []byte(payload)}
	return Update{Stream: stream, At: at, Entry: &e}
}

func ack(stream string, index uint64, replica, digest string, at int64) Update {
	a := Ack{Index: index, Replica: replica, Digest: digest}
	return Update{Stream: stream, At: at, Ack: &a}
}

func seal(stream string, last uint64, at int64) Update {
	s := Seal{LastIndex: last}
	return Update{Stream: stream, At: at, Seal: &s}
}

func TestValidationAndOpenSemantics(t *testing.T) {
	for _, max := range []int{0, -1} {
		if _, err := New(Options{MaxBufferedBytes: max}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("New(%d)=%v", max, err)
		}
	}
	q := mustNew(t, 100)
	bad := []StreamConfig{
		{},
		{ID: "s", Voters: []string{"r1"}, Quorum: 0},
		{ID: "s", Voters: []string{"r1"}, Quorum: 2},
		{ID: "s", Voters: []string{""}, Quorum: 1},
		{ID: "s", Voters: []string{"r1", "r1"}, Quorum: 1},
	}
	for _, c := range bad {
		if err := q.Open(c, 0); !errors.Is(err, ErrInvalid) {
			t.Fatalf("Open(%+v)=%v", c, err)
		}
	}
	mustOpen(t, q, "s", []string{"r2", "r1"}, 2, 5)
	if err := q.Open(StreamConfig{ID: "s", Voters: []string{"r1", "r2"}, Quorum: 2}, 6); err != nil {
		t.Fatalf("idempotent open: %v", err)
	}
	if err := q.Open(StreamConfig{ID: "s", Voters: []string{"r1", "r2"}, Quorum: 1}, 7); !errors.Is(err, ErrConflict) {
		t.Fatalf("config conflict=%v", err)
	}
	if err := q.Open(StreamConfig{ID: "s", Voters: []string{"r1", "r2"}, Quorum: 2}, 4); !errors.Is(err, ErrTime) {
		t.Fatalf("time rollback=%v", err)
	}
	invalid := []Update{
		{},
		{Stream: "s", At: -1, Entry: &Entry{Index: 1, Digest: "d"}},
		{Stream: "s", At: 6, Entry: &Entry{Index: 0, Digest: "d"}},
		{Stream: "s", At: 6, Entry: &Entry{Index: 1}},
		{Stream: "s", At: 6, Ack: &Ack{Index: 1, Replica: "r1", Digest: ""}},
		{Stream: "s", At: 6, Entry: &Entry{Index: 1, Digest: "d"}, Seal: &Seal{LastIndex: 1}},
		{Stream: "s", At: 6, Ack: &Ack{Index: 1, Replica: "outsider", Digest: "d"}},
	}
	for _, u := range invalid {
		if got, err := q.Apply(u); !errors.Is(err, ErrInvalid) || !reflect.DeepEqual(got, Outcome{}) {
			t.Fatalf("invalid update got=%+v err=%v", got, err)
		}
	}
	if _, err := q.Apply(ent("missing", 1, "d", "x", 0)); !errors.Is(err, ErrUnknownStream) {
		t.Fatalf("unknown stream=%v", err)
	}
}

func TestAckBeforeEntryAndContinuousCommit(t *testing.T) {
	q := mustNew(t, 100)
	mustOpen(t, q, "s", []string{"a", "b", "c"}, 2, 0)
	for _, u := range []Update{
		ack("s", 2, "a", "d2", 1),
		ack("s", 2, "b", "d2", 1),
		ent("s", 2, "d2", "two", 1),
		ent("s", 1, "d1", "one", 1),
		ack("s", 1, "a", "d1", 1),
	} {
		o, err := q.Apply(u)
		if err != nil || len(o.Committed) != 0 {
			t.Fatalf("premature commit: %+v %v", o, err)
		}
	}
	o, err := q.Apply(ack("s", 1, "b", "d1", 1))
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Committed) != 2 || o.Committed[0].Index != 1 || o.Committed[1].Index != 2 {
		t.Fatalf("continuous order: %+v", o)
	}
	if string(o.Committed[0].Payload) != "one" || string(o.Committed[1].Payload) != "two" {
		t.Fatalf("payloads: %+v", o.Committed)
	}
	if s := q.Snapshot(); s.Streams[0].Committed != 2 || s.BufferedBytes != 0 || s.Streams[0].PendingAcks != 0 {
		t.Fatalf("snapshot: %+v", s)
	}
}

func TestDisagreeingAcksIdempotencyAndConflicts(t *testing.T) {
	q := mustNew(t, 100)
	mustOpen(t, q, "s", []string{"a", "b", "c"}, 2, 0)
	if _, err := q.Apply(ack("s", 1, "a", "wrong", 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(ack("s", 1, "a", "wrong", 1)); err != nil {
		t.Fatalf("ack duplicate: %v", err)
	}
	if _, err := q.Apply(ack("s", 1, "a", "right", 1)); !errors.Is(err, ErrConflict) {
		t.Fatalf("ack conflict=%v", err)
	}
	p := []byte("payload")
	u := Update{Stream: "s", At: 1, Entry: &Entry{Index: 1, Digest: "right", Payload: p}}
	if _, err := q.Apply(u); err != nil {
		t.Fatal(err)
	}
	p[0] = 'X'
	if _, err := q.Apply(ent("s", 1, "right", "payload", 1)); err != nil {
		t.Fatalf("entry duplicate/input ownership: %v", err)
	}
	if _, err := q.Apply(ent("s", 1, "right", "different", 1)); !errors.Is(err, ErrConflict) {
		t.Fatalf("payload conflict=%v", err)
	}
	if _, err := q.Apply(ack("s", 1, "b", "right", 1)); err != nil {
		t.Fatal(err)
	}
	o, err := q.Apply(ack("s", 1, "c", "right", 1))
	if err != nil || len(o.Committed) != 1 {
		t.Fatalf("commit: %+v %v", o, err)
	}
	o.Committed[0].Payload[0] = 'Z'
	if _, err := q.Apply(ent("s", 1, "right", "ignored-after-commit", 2)); err != nil {
		t.Fatalf("committed duplicate=%v", err)
	}
	if _, err := q.Apply(ent("s", 1, "other", "x", 2)); !errors.Is(err, ErrConflict) {
		t.Fatalf("committed conflict=%v", err)
	}
}

func TestSealEarlyBoundaryAndCompletion(t *testing.T) {
	q := mustNew(t, 100)
	mustOpen(t, q, "empty", []string{"a"}, 1, 0)
	o, err := q.Apply(seal("empty", 0, 1))
	if err != nil || !o.Completed {
		t.Fatalf("empty complete: %+v %v", o, err)
	}
	o, err = q.Apply(seal("empty", 0, 2))
	if err != nil || o.Completed {
		t.Fatalf("completion repeated: %+v %v", o, err)
	}
	if _, err := q.Apply(ent("empty", 1, "d", "x", 2)); !errors.Is(err, ErrSealed) {
		t.Fatalf("entry beyond empty seal=%v", err)
	}

	mustOpen(t, q, "s", []string{"a"}, 1, 0)
	if _, err := q.Apply(ent("s", 2, "d2", "two", 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(seal("s", 1, 1)); !errors.Is(err, ErrConflict) {
		t.Fatalf("seal below observed=%v", err)
	}
	if _, err := q.Apply(seal("s", 2, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(ack("s", 3, "a", "d3", 1)); !errors.Is(err, ErrSealed) {
		t.Fatalf("ack beyond seal=%v", err)
	}
	for _, u := range []Update{ack("s", 2, "a", "d2", 1), ent("s", 1, "d1", "one", 1)} {
		if _, err := q.Apply(u); err != nil {
			t.Fatal(err)
		}
	}
	o, err = q.Apply(ack("s", 1, "a", "d1", 1))
	if err != nil || !o.Completed || len(o.Committed) != 2 {
		t.Fatalf("sealed completion: %+v %v", o, err)
	}
}

func TestBatchAtomicityErrorOrderAndCapacity(t *testing.T) {
	q := mustNew(t, 4)
	mustOpen(t, q, "s", []string{"a"}, 1, 0)
	if _, err := q.Apply(ent("s", 1, "d1", "aa", 1)); err != nil {
		t.Fatal(err)
	}
	before := q.Snapshot()
	out, err := q.ApplyBatch([]Update{
		ent("s", 1, "conflict", "aa", 2),
		{Stream: "s", At: 2},
	})
	if !errors.Is(err, ErrConflict) || out != nil {
		t.Fatalf("first error: out=%v err=%v", out, err)
	}
	if !reflect.DeepEqual(before, q.Snapshot()) {
		t.Fatal("state changed after failed batch")
	}

	out, err = q.ApplyBatch([]Update{
		ack("s", 1, "a", "d1", 2),
		{Stream: "s", At: 2},
	})
	if !errors.Is(err, ErrInvalid) || out != nil || !reflect.DeepEqual(before, q.Snapshot()) {
		t.Fatalf("rollback after prepared output: out=%v err=%v state=%+v", out, err, q.Snapshot())
	}

	q2 := mustNew(t, 3)
	mustOpen(t, q2, "s", []string{"a"}, 1, 0)
	out, err = q2.ApplyBatch([]Update{
		ent("s", 1, "d", "four", 1),
		ack("s", 1, "a", "d", 1),
	})
	if err != nil || len(out) != 2 || len(out[1].Committed) != 1 || q2.Snapshot().BufferedBytes != 0 {
		t.Fatalf("temporary overflow: %+v %v %+v", out, err, q2.Snapshot())
	}
	if got, err := q2.Apply(ent("s", 2, "d2", "four", 2)); !errors.Is(err, ErrCapacity) || !reflect.DeepEqual(got, Outcome{}) {
		t.Fatalf("final capacity: %+v %v", got, err)
	}
	if q2.Snapshot().Streams[0].PendingEntries != 0 {
		t.Fatal("capacity failure mutated state")
	}
	empty, err := q2.ApplyBatch(nil)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty batch: %#v %v", empty, err)
	}
}

func TestExpirySnapshotOrderingAndOwnership(t *testing.T) {
	q := mustNew(t, 100)
	mustOpen(t, q, "z", []string{"b", "a"}, 1, 5)
	mustOpen(t, q, "a", []string{"a"}, 1, 4)
	mustOpen(t, q, "m", []string{"a"}, 1, 5)
	if _, err := q.Apply(ent("a", 1, "d", "abc", 4)); err != nil {
		t.Fatal(err)
	}
	s := q.Snapshot()
	if len(s.Streams) != 3 || s.Streams[0].ID != "a" || s.Streams[1].ID != "m" || s.Streams[2].ID != "z" {
		t.Fatalf("snapshot order: %+v", s.Streams)
	}
	if !reflect.DeepEqual(s.Streams[2].Voters, []string{"a", "b"}) {
		t.Fatalf("voter canonicalization: %+v", s.Streams[2].Voters)
	}
	s.Streams[2].Voters[0] = "mutated"
	if q.Snapshot().Streams[2].Voters[0] != "a" {
		t.Fatal("snapshot aliases voters")
	}
	if _, err := q.ExpireBefore(-1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid cutoff=%v", err)
	}
	expired, err := q.ExpireBefore(5)
	if err != nil || !reflect.DeepEqual(expired, []ExpiredStream{{Stream: "a", Committed: 0, BufferedBytes: 3}}) {
		t.Fatalf("expired: %+v %v", expired, err)
	}
	if q.Snapshot().BufferedBytes != 0 || len(q.Snapshot().Streams) != 2 {
		t.Fatalf("expiry accounting: %+v", q.Snapshot())
	}
	if err := q.Open(StreamConfig{ID: "a", Voters: []string{"x"}, Quorum: 1}, 1); err != nil {
		t.Fatalf("reopen expired: %v", err)
	}
}

func TestConcurrentCalls(t *testing.T) {
	q := mustNew(t, 10_000)
	mustOpen(t, q, "s", []string{"a", "b"}, 2, 0)
	var wg sync.WaitGroup
	errCh := make(chan error, 60)
	for i := 1; i <= 20; i++ {
		i := uint64(i)
		wg.Add(3)
		go func() { defer wg.Done(); _, err := q.Apply(ent("s", i, "d", "x", 1)); errCh <- err }()
		go func() { defer wg.Done(); _, err := q.Apply(ack("s", i, "a", "d", 1)); errCh <- err }()
		go func() { defer wg.Done(); _, err := q.Apply(ack("s", i, "b", "d", 1)); errCh <- err }()
	}
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = q.Snapshot() }()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	s := q.Snapshot()
	if len(s.Streams) != 1 || s.Streams[0].Committed != 20 || s.BufferedBytes != 0 {
		t.Fatalf("final concurrent snapshot: %+v", s)
	}
}
