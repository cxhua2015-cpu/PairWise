package topiclog

import (
	"errors"
	"fmt"
	"hash/fnv"
	"reflect"
	"sync"
	"testing"
)

func fnvPartition(key string, partitions int) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return int(h.Sum32() % uint32(partitions))
}

func TestPartitioningMatchesFNV(t *testing.T) {
	l := testLog(t)
	keys := []string{"", "a", "customer-1", "客户", "z9_._-"}
	for _, k := range keys {
		recs, _, err := l.AppendBatch([]Input{{Topic: "t", Key: k}})
		if err != nil {
			t.Fatal(err)
		}
		want := 0
		if k != "" {
			want = fnvPartition(k, 4)
		}
		if recs[0].Partition != want {
			t.Errorf("key %q: partition=%d want %d", k, recs[0].Partition, want)
		}
	}
	// Same key always lands on the same partition; offsets are per-partition consecutive.
	recs, _, err := l.AppendBatch([]Input{{Topic: "t", Key: "a"}, {Topic: "t", Key: "a"}})
	if err != nil {
		t.Fatal(err)
	}
	p := fnvPartition("a", 4)
	if recs[0].Partition != p || recs[1].Partition != p || recs[1].Offset != recs[0].Offset+1 {
		t.Fatalf("recs=%+v", recs)
	}
}

func TestAppendBatchRollbackOnCapacity(t *testing.T) {
	l, _ := New(Options{Partitions: 2, MaxTopics: 1, MaxRecords: 3, MaxPayloadBytes: 8})
	if _, _, err := l.AppendBatch([]Input{{Topic: "a", Payload: []byte("12")}}); err != nil {
		t.Fatal(err)
	}
	before := l.Snapshot()
	// Valid batch that overflows records and payload at final state.
	_, _, err := l.AppendBatch([]Input{
		{Topic: "a", Payload: []byte("34")},
		{Topic: "a", Payload: []byte("56")},
		{Topic: "a", Payload: []byte("78")},
	})
	if !errors.Is(err, ErrCapacity) {
		t.Fatalf("err=%v", err)
	}
	if !reflect.DeepEqual(before, l.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
	// Batch that would create a second topic must roll back the topic creation.
	_, _, err = l.AppendBatch([]Input{{Topic: "b"}, {Topic: "b"}, {Topic: "b"}, {Topic: "b"}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatalf("err=%v", err)
	}
	if !reflect.DeepEqual(before, l.Snapshot()) {
		t.Fatal("state changed after topic capacity failure")
	}
	// Empty batch succeeds with current generation.
	_, g, err := l.AppendBatch(nil)
	if err != nil || g != before.Generation {
		t.Fatalf("empty batch g=%d err=%v", g, err)
	}
}

func TestCommitBoundaries(t *testing.T) {
	l := testLog(t)
	// Commit offset 0 on unknown topic is allowed (high watermark 0).
	if _, err := l.CommitBatch([]Commit{{Group: "g", Topic: "nope", Partition: 0, Offset: 0}}); err != nil {
		t.Fatalf("commit zero on unknown topic: %v", err)
	}
	// Any positive offset on unknown topic is ahead.
	if _, err := l.CommitBatch([]Commit{{Group: "g", Topic: "nope", Partition: 0, Offset: 1}}); !errors.Is(err, ErrOffsetAhead) {
		t.Fatalf("err=%v", err)
	}
	recs, _, _ := l.AppendBatch([]Input{{Topic: "t"}, {Topic: "t"}})
	p := recs[0].Partition
	// In-batch ordering: a later commit sees the earlier one (regression within batch rolls back all).
	before := l.Snapshot()
	_, err := l.CommitBatch([]Commit{
		{Group: "g", Topic: "t", Partition: p, Offset: 2},
		{Group: "g", Topic: "t", Partition: p, Offset: 1},
	})
	if !errors.Is(err, ErrOffsetRegression) {
		t.Fatalf("err=%v", err)
	}
	if !reflect.DeepEqual(before, l.Snapshot()) {
		t.Fatal("commit batch not rolled back")
	}
	// Equal commit is allowed and still bumps generation.
	g1, err := l.CommitBatch([]Commit{{Group: "g", Topic: "t", Partition: p, Offset: 2}})
	if err != nil {
		t.Fatal(err)
	}
	g2, err := l.CommitBatch([]Commit{{Group: "g", Topic: "t", Partition: p, Offset: 2}})
	if err != nil || g2 != g1+1 {
		t.Fatalf("equal commit g1=%d g2=%d err=%v", g1, g2, err)
	}
	// Empty commit batch returns current generation without bump.
	g3, err := l.CommitBatch(nil)
	if err != nil || g3 != g2 {
		t.Fatalf("empty commit g=%d want %d err=%v", g3, g2, err)
	}
	// Invalid partition is structural.
	if _, err := l.CommitBatch([]Commit{{Group: "g", Topic: "t", Partition: -1}}); !errors.Is(err, ErrInvalidPartition) {
		t.Fatalf("err=%v", err)
	}
}

func TestTrimBoundaries(t *testing.T) {
	l := testLog(t)
	if _, err := l.Trim("missing", 0, 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
	if _, err := l.Trim("bad/name", 0, 0); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("err=%v", err)
	}
	recs, _, _ := l.AppendBatch([]Input{{Topic: "t"}, {Topic: "t"}, {Topic: "t"}})
	p := recs[0].Partition
	if _, err := l.Trim("t", p, 3); !errors.Is(err, ErrUnsafeTrim) {
		t.Fatalf("no consumers: err=%v", err)
	}
	if _, err := l.CommitBatch([]Commit{{Group: "g", Topic: "t", Partition: p, Offset: 3}}); err != nil {
		t.Fatal(err)
	}
	// through beyond high offset is rejected while commits lag behind it.
	if _, err := l.Trim("t", p, 1000); !errors.Is(err, ErrUnsafeTrim) {
		t.Fatalf("through beyond commits: err=%v", err)
	}
	// through equal to the committed high offset removes everything.
	g, err := l.Trim("t", p, 3)
	if err != nil {
		t.Fatal(err)
	}
	s := l.Snapshot()
	if s.Generation != g || s.UsedRecords != 0 || s.UsedPayloadBytes != 0 {
		t.Fatalf("snapshot=%+v", s)
	}
	if s.Topics[0].Partitions[p].HighOffset != 3 {
		t.Fatalf("high offset regressed: %+v", s.Topics[0].Partitions[p])
	}
	// Trim with through=0 on empty partition still bumps generation.
	if _, err := l.Trim("t", p, 0); err != nil {
		t.Fatal(err)
	}
	// A group committed on a different partition does not gate this one.
	if _, err := l.Trim("t", (p+1)%4, 0); !errors.Is(err, ErrUnsafeTrim) {
		t.Fatalf("other partition: err=%v", err)
	}
}

func TestReadPagination(t *testing.T) {
	l := testLog(t)
	for i := 0; i < 5; i++ {
		if _, _, err := l.AppendBatch([]Input{{Topic: "t", Payload: []byte{byte('a' + i)}}}); err != nil {
			t.Fatal(err)
		}
	}
	r, err := l.Read("t", 0, 0, 2)
	if err != nil || len(r.Records) != 2 || r.Records[0].Offset != 1 || r.Records[1].Offset != 2 {
		t.Fatalf("page1=%+v err=%v", r, err)
	}
	r, err = l.Read("t", 0, 2, 1000)
	if err != nil || len(r.Records) != 3 || r.Records[0].Offset != 3 {
		t.Fatalf("page2=%+v err=%v", r, err)
	}
	r, err = l.Read("t", 0, 5, 10)
	if err != nil || len(r.Records) != 0 || r.Generation != l.Snapshot().Generation {
		t.Fatalf("past-end=%+v err=%v", r, err)
	}
	if _, err := l.Read("t", 0, 0, 1001); !errors.Is(err, ErrInvalidLimit) {
		t.Fatalf("limit err=%v", err)
	}
}

func TestOwnershipIsolation(t *testing.T) {
	l := testLog(t)
	in := []byte("abc")
	recs, _, _ := l.AppendBatch([]Input{{Topic: "t", Payload: in}})
	in[0] = 'X'
	recs[0].Payload[0] = 'Y'
	r, _ := l.Read("t", 0, 0, 1)
	if string(r.Records[0].Payload) != "abc" {
		t.Fatalf("input/result alias: %q", r.Records[0].Payload)
	}
	r.Records[0].Payload[0] = 'Z'
	s := l.Snapshot()
	if string(s.Topics[0].Partitions[0].Records[0].Payload) != "abc" {
		t.Fatal("read aliases internal state")
	}
	s.Topics[0].Partitions[0].Records[0].Payload[0] = 'W'
	r2, _ := l.Read("t", 0, 0, 1)
	if string(r2.Records[0].Payload) != "abc" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestConcurrentAccess(t *testing.T) {
	l, _ := New(Options{Partitions: 8, MaxTopics: 64, MaxRecords: 1_000_000, MaxPayloadBytes: 64 << 20})
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			topic := fmt.Sprintf("topic-%d", w%4)
			group := fmt.Sprintf("group-%d", w%4)
			for i := 0; i < 200; i++ {
				recs, _, err := l.AppendBatch([]Input{{Topic: topic, Key: fmt.Sprintf("k-%d", i), Payload: []byte("v")}})
				if err != nil {
					continue
				}
				_, _ = l.Read(topic, recs[0].Partition, 0, 10)
				_, _ = l.CommitBatch([]Commit{{Group: group, Topic: topic, Partition: recs[0].Partition, Offset: recs[0].Offset}})
				_, _ = l.Trim(topic, recs[0].Partition, recs[0].Offset)
				_ = l.Snapshot()
			}
		}(w)
	}
	wg.Wait()
	s := l.Snapshot()
	if s.UsedRecords < 0 || s.UsedPayloadBytes < 0 {
		t.Fatalf("negative usage: %+v", s)
	}
	// Offsets per partition remain strictly continuous with high watermark.
	for _, tv := range s.Topics {
		for _, pv := range tv.Partitions {
			for i, r := range pv.Records {
				want := pv.HighOffset - uint64(len(pv.Records)) + 1 + uint64(i)
				if r.Offset != want {
					t.Fatalf("gap in %s/%d: got %d want %d", tv.Topic, pv.Partition, r.Offset, want)
				}
			}
		}
	}
}
