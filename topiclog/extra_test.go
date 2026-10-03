package topiclog

import (
	"errors"
	"hash/fnv"
	"reflect"
	"sync"
	"testing"
)

func newLog(t *testing.T, o Options) *Log {
	t.Helper()
	l, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestPartitioningStableAndFNV(t *testing.T) {
	l := newLog(t, Options{Partitions: 8, MaxTopics: 4, MaxRecords: 64, MaxPayloadBytes: 1 << 20})
	keys := []string{"alpha", "beta", "gamma", "delta", "epsilon"}
	seen := map[string]int{}
	for round := 0; round < 3; round++ {
		recs, _, err := l.AppendBatch([]Input{
			{Topic: "t", Key: keys[0]}, {Topic: "t", Key: keys[1]},
			{Topic: "t", Key: keys[2]}, {Topic: "t", Key: keys[3]},
			{Topic: "t", Key: keys[4]},
		})
		if err != nil {
			t.Fatal(err)
		}
		for i, r := range recs {
			h := fnv.New32a()
			h.Write([]byte(keys[i]))
			want := int(h.Sum32() % 8)
			if r.Partition != want {
				t.Fatalf("key %q partition %d want %d", keys[i], r.Partition, want)
			}
			if prev, ok := seen[keys[i]]; ok && prev != r.Partition {
				t.Fatalf("key %q moved partition %d -> %d", keys[i], prev, r.Partition)
			}
			seen[keys[i]] = r.Partition
		}
	}
	// Offsets strictly contiguous per partition.
	s := l.Snapshot()
	for _, tv := range s.Topics {
		for _, pv := range tv.Partitions {
			for i, r := range pv.Records {
				if want := pv.HighOffset - uint64(len(pv.Records)) + uint64(i) + 1; r.Offset != want {
					t.Fatalf("partition %d record %d offset %d want %d", pv.Partition, i, r.Offset, want)
				}
			}
		}
	}
}

func TestAppendBatchRollbackOnSemanticFailure(t *testing.T) {
	l := newLog(t, Options{Partitions: 2, MaxTopics: 2, MaxRecords: 4, MaxPayloadBytes: 8})
	if _, _, err := l.AppendBatch([]Input{{Topic: "a", Payload: []byte("aa")}}); err != nil {
		t.Fatal(err)
	}
	before := l.Snapshot()
	// Structural failure late in the batch: earlier valid items must not apply.
	_, _, err := l.AppendBatch([]Input{{Topic: "b", Payload: []byte("x")}, {Topic: "bad name"}})
	if !errors.Is(err, ErrInvalidName) {
		t.Fatalf("err=%v", err)
	}
	if !reflect.DeepEqual(before, l.Snapshot()) {
		t.Fatal("state changed after structural failure")
	}
	// Capacity failure only visible in final state (record count exceeded).
	_, _, err = l.AppendBatch([]Input{{Topic: "a"}, {Topic: "a"}, {Topic: "a"}, {Topic: "a"}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatalf("err=%v", err)
	}
	if !reflect.DeepEqual(before, l.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
	// Payload byte capacity exceeded.
	_, _, err = l.AppendBatch([]Input{{Topic: "a", Payload: make([]byte, 8)}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatalf("err=%v", err)
	}
	if !reflect.DeepEqual(before, l.Snapshot()) {
		t.Fatal("state changed after payload capacity failure")
	}
	// Topic count capacity.
	_, _, err = l.AppendBatch([]Input{{Topic: "b"}, {Topic: "c"}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatalf("err=%v", err)
	}
	if !reflect.DeepEqual(before, l.Snapshot()) {
		t.Fatal("state changed after topic capacity failure")
	}
}

func TestEmptyBatchesDoNotAdvanceGeneration(t *testing.T) {
	l := newLog(t, Options{Partitions: 2, MaxTopics: 2, MaxRecords: 4, MaxPayloadBytes: 16})
	if _, g, err := l.AppendBatch(nil); err != nil || g != 0 {
		t.Fatalf("empty append g=%d err=%v", g, err)
	}
	if g, err := l.CommitBatch(nil); err != nil || g != 0 {
		t.Fatalf("empty commit g=%d err=%v", g, err)
	}
	if _, g, err := l.AppendBatch([]Input{{Topic: "a"}}); err != nil || g != 1 {
		t.Fatalf("append g=%d err=%v", g, err)
	}
	if _, g, err := l.AppendBatch(nil); err != nil || g != 1 {
		t.Fatalf("empty append g=%d err=%v", g, err)
	}
	// Equal-value commit still advances generation.
	if g, err := l.CommitBatch([]Commit{{Group: "g", Topic: "a", Partition: 0, Offset: 1}}); err != nil || g != 2 {
		t.Fatalf("commit g=%d err=%v", g, err)
	}
	if g, err := l.CommitBatch([]Commit{{Group: "g", Topic: "a", Partition: 0, Offset: 1}}); err != nil || g != 3 {
		t.Fatalf("equal commit g=%d err=%v", g, err)
	}
}

func TestCommitBatchAtomicRollback(t *testing.T) {
	l := newLog(t, Options{Partitions: 2, MaxTopics: 2, MaxRecords: 8, MaxPayloadBytes: 16})
	recs, _, _ := l.AppendBatch([]Input{{Topic: "t", Key: "", Payload: []byte("a")}, {Topic: "t", Key: "", Payload: []byte("b")}})
	if _, err := l.CommitBatch([]Commit{{Group: "g", Topic: "t", Partition: 0, Offset: 2}}); err != nil {
		t.Fatal(err)
	}
	before := l.Snapshot()
	// Second commit regresses: whole batch must roll back, first commit lost too.
	_, err := l.CommitBatch([]Commit{
		{Group: "h", Topic: "t", Partition: 0, Offset: 2},
		{Group: "g", Topic: "t", Partition: 0, Offset: 1},
	})
	if !errors.Is(err, ErrOffsetRegression) {
		t.Fatalf("err=%v", err)
	}
	if !reflect.DeepEqual(before, l.Snapshot()) {
		t.Fatal("state changed after rolled-back commit batch")
	}
	// Ahead-of-high commit against unknown topic (high = 0).
	_, err = l.CommitBatch([]Commit{{Group: "h", Topic: "missing", Partition: 0, Offset: 1}})
	if !errors.Is(err, ErrOffsetAhead) {
		t.Fatalf("err=%v", err)
	}
	// Offset 0 commit to unknown topic is allowed.
	if _, err := l.CommitBatch([]Commit{{Group: "h", Topic: "missing", Partition: 0, Offset: 0}}); err != nil {
		t.Fatalf("err=%v", err)
	}
	_ = recs
}

func TestTrimBoundaries(t *testing.T) {
	l := newLog(t, Options{Partitions: 2, MaxTopics: 2, MaxRecords: 8, MaxPayloadBytes: 16})
	if _, err := l.Trim("nope", 0, 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
	recs, _, _ := l.AppendBatch([]Input{{Topic: "t", Key: "", Payload: []byte("aa")}, {Topic: "t", Key: "", Payload: []byte("bb")}})
	if _, err := l.Trim("t", 0, 1); !errors.Is(err, ErrUnsafeTrim) {
		t.Fatalf("no-group trim err=%v", err)
	}
	if _, err := l.CommitBatch([]Commit{{Group: "g", Topic: "t", Partition: 0, Offset: 1}}); err != nil {
		t.Fatal(err)
	}
	// through=0 trims nothing but advances generation.
	g, err := l.Trim("t", 0, 0)
	if err != nil || g != 3 {
		t.Fatalf("trim0 g=%d err=%v", g, err)
	}
	if s := l.Snapshot(); s.UsedRecords != 2 || s.UsedPayloadBytes != 4 {
		t.Fatalf("snapshot=%+v", s)
	}
	// through equal to high offset trims everything.
	if _, err := l.CommitBatch([]Commit{{Group: "g", Topic: "t", Partition: 0, Offset: 2}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Trim("t", 0, 2); err != nil {
		t.Fatalf("trim-all err=%v", err)
	}
	s := l.Snapshot()
	if s.UsedRecords != 0 || s.UsedPayloadBytes != 0 || s.Topics[0].Partitions[0].HighOffset != 2 {
		t.Fatalf("snapshot=%+v", s)
	}
	// Offsets not reused after trim.
	recs2, _, err := l.AppendBatch([]Input{{Topic: "t", Key: ""}})
	if err != nil || recs2[0].Offset != 3 {
		t.Fatalf("recs=%+v err=%v", recs2, err)
	}
	_ = recs
}

func TestReadPaginationAndOwnership(t *testing.T) {
	l := newLog(t, Options{Partitions: 1, MaxTopics: 2, MaxRecords: 8, MaxPayloadBytes: 16})
	in := []byte("abc")
	if _, _, err := l.AppendBatch([]Input{
		{Topic: "t", Payload: in}, {Topic: "t", Payload: []byte("def")}, {Topic: "t", Payload: []byte("ghi")},
	}); err != nil {
		t.Fatal(err)
	}
	in[0] = 'X' // caller mutation must not affect stored data
	r, err := l.Read("t", 0, 1, 1)
	if err != nil || len(r.Records) != 1 || r.Records[0].Offset != 2 || string(r.Records[0].Payload) != "def" {
		t.Fatalf("read=%+v err=%v", r, err)
	}
	r.Records[0].Payload[0] = 'Y'
	r2, err := l.Read("t", 0, 0, 1000)
	if err != nil || len(r2.Records) != 3 || string(r2.Records[0].Payload) != "abc" || string(r2.Records[1].Payload) != "def" {
		t.Fatalf("read2=%+v err=%v", r2, err)
	}
	if r2.Generation != 1 {
		t.Fatalf("generation=%d", r2.Generation)
	}
	// Mutating snapshot output must not affect internal state.
	s := l.Snapshot()
	s.Topics[0].Partitions[0].Records[0].Payload[0] = 'Z'
	if got := l.Snapshot().Topics[0].Partitions[0].Records[0].Payload; string(got) != "abc" {
		t.Fatalf("snapshot alias: %q", got)
	}
}

func TestConcurrentAccess(t *testing.T) {
	l := newLog(t, Options{Partitions: 4, MaxTopics: 8, MaxRecords: 100000, MaxPayloadBytes: 64 << 20})
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			key := string(rune('a'+w)) + "-key"
			for i := 0; i < 50; i++ {
				recs, _, err := l.AppendBatch([]Input{{Topic: "t", Key: key, Payload: []byte("v")}})
				if err != nil {
					t.Error(err)
					return
				}
				if _, err := l.Read("t", recs[0].Partition, 0, 10); err != nil {
					t.Error(err)
					return
				}
				if _, err := l.CommitBatch([]Commit{{Group: string(rune('a' + w)), Topic: "t", Partition: recs[0].Partition, Offset: recs[0].Offset}}); err != nil {
					t.Error(err)
					return
				}
				_ = l.Snapshot()
			}
		}(w)
	}
	wg.Wait()
	// Per-partition offsets remain contiguous from 1.
	s := l.Snapshot()
	if s.UsedRecords != 400 {
		t.Fatalf("used=%d", s.UsedRecords)
	}
	for _, tv := range s.Topics {
		for _, pv := range tv.Partitions {
			for i, r := range pv.Records {
				if r.Offset != uint64(i)+1 {
					t.Fatalf("partition %d gap at %d: offset %d", pv.Partition, i, r.Offset)
				}
			}
		}
	}
}
