package topiclog

import (
	"errors"
	"reflect"
	"testing"
)

func testLog(t *testing.T) *Log {
	t.Helper()
	l, err := New(Options{Partitions: 4, MaxTopics: 8, MaxRecords: 32, MaxPayloadBytes: 128})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestOptionsAndInputValidation(t *testing.T) {
	for _, o := range []Options{{}, {0, 1, 1, 1}, {65, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0}} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("New(%+v)=%v", o, err)
		}
	}
	l := testLog(t)
	for _, tc := range []struct {
		in   Input
		want error
	}{
		{Input{Topic: "bad/name"}, ErrInvalidName},
		{Input{Topic: "ok", Key: string(make([]byte, 257))}, ErrInvalidKey},
		{Input{Topic: "ok", Payload: make([]byte, 1<<20+1)}, ErrPayloadTooLarge},
	} {
		if _, _, err := l.AppendBatch([]Input{tc.in}); !errors.Is(err, tc.want) {
			t.Errorf("Append=%v want %v", err, tc.want)
		}
	}
}

func TestAppendOffsetsPartitionAndOwnership(t *testing.T) {
	l := testLog(t)
	p := []byte("one")
	recs, g, err := l.AppendBatch([]Input{{Topic: "t", Key: "", Payload: p}, {Topic: "t", Key: "", Payload: []byte("two")}})
	if err != nil || g != 1 || len(recs) != 2 || recs[0].Partition != 0 || recs[0].Offset != 1 || recs[1].Offset != 2 {
		t.Fatalf("recs=%+v g=%d err=%v", recs, g, err)
	}
	p[0] = 'X'
	recs[0].Payload[0] = 'Y'
	r, err := l.Read("t", 0, 0, 10)
	if err != nil || len(r.Records) != 2 || string(r.Records[0].Payload) != "one" {
		t.Fatalf("read=%+v err=%v", r, err)
	}
	r.Records[0].Payload[0] = 'Z'
	if string(l.Snapshot().Topics[0].Partitions[0].Records[0].Payload) != "one" {
		t.Fatal("return alias")
	}
}

func TestAppendAtomicFinalCapacity(t *testing.T) {
	l, _ := New(Options{Partitions: 2, MaxTopics: 1, MaxRecords: 2, MaxPayloadBytes: 4})
	_, _, err := l.AppendBatch([]Input{{Topic: "a", Payload: []byte("aa")}, {Topic: "a", Payload: []byte("bb")}})
	if err != nil {
		t.Fatal(err)
	}
	before := l.Snapshot()
	_, _, err = l.AppendBatch([]Input{{Topic: "b"}, {Topic: "bad/name"}})
	if !errors.Is(err, ErrInvalidName) || !reflect.DeepEqual(before, l.Snapshot()) {
		t.Fatalf("err=%v state changed", err)
	}
	_, _, err = l.AppendBatch([]Input{{Topic: "a", Payload: []byte("x")}})
	if !errors.Is(err, ErrCapacity) || !reflect.DeepEqual(before, l.Snapshot()) {
		t.Fatalf("capacity=%v", err)
	}
}

func TestCommitAtomicAndBounds(t *testing.T) {
	l := testLog(t)
	recs, _, _ := l.AppendBatch([]Input{{Topic: "t", Key: "", Payload: []byte("a")}, {Topic: "t", Key: "", Payload: []byte("b")}})
	g, err := l.CommitBatch([]Commit{{Group: "g", Topic: "t", Partition: 0, Offset: 2}})
	if err != nil || g != 2 {
		t.Fatalf("commit=(%d,%v)", g, err)
	}
	before := l.Snapshot()
	_, err = l.CommitBatch([]Commit{{Group: "g", Topic: "t", Partition: 0, Offset: 1}, {Group: "bad/name", Topic: "t", Partition: 0, Offset: 2}})
	if !errors.Is(err, ErrInvalidName) || !reflect.DeepEqual(before, l.Snapshot()) {
		t.Fatalf("structural=%v", err)
	}
	_, err = l.CommitBatch([]Commit{{Group: "g", Topic: "t", Partition: 0, Offset: 1}})
	if !errors.Is(err, ErrOffsetRegression) {
		t.Fatalf("regression=%v", err)
	}
	_, err = l.CommitBatch([]Commit{{Group: "x", Topic: "t", Partition: recs[0].Partition, Offset: 3}})
	if !errors.Is(err, ErrOffsetAhead) {
		t.Fatalf("ahead=%v", err)
	}
}

func TestTrimSafetyAndHighOffset(t *testing.T) {
	l := testLog(t)
	recs, _, _ := l.AppendBatch([]Input{{Topic: "t"}, {Topic: "t"}})
	p := recs[0].Partition
	if _, err := l.Trim("t", p, 1); !errors.Is(err, ErrUnsafeTrim) {
		t.Fatalf("unsafe=%v", err)
	}
	_, _ = l.CommitBatch([]Commit{{Group: "a", Topic: "t", Partition: p, Offset: 2}, {Group: "b", Topic: "t", Partition: p, Offset: 1}})
	if _, err := l.Trim("t", p, 2); !errors.Is(err, ErrUnsafeTrim) {
		t.Fatalf("unsafe2=%v", err)
	}
	_, _ = l.CommitBatch([]Commit{{Group: "b", Topic: "t", Partition: p, Offset: 2}})
	g, err := l.Trim("t", p, 2)
	if err != nil || g != 4 {
		t.Fatalf("trim=(%d,%v)", g, err)
	}
	s := l.Snapshot()
	if s.UsedRecords != 0 || s.Topics[0].Partitions[p].HighOffset != 2 {
		t.Fatalf("snapshot=%+v", s)
	}
	recs, _, _ = l.AppendBatch([]Input{{Topic: "t"}})
	if recs[0].Offset != 3 {
		t.Fatalf("offset reused: %+v", recs[0])
	}
}

func TestReadValidationAndSnapshotOrder(t *testing.T) {
	l := testLog(t)
	if _, err := l.Read("bad/name", 0, 0, 1); !errors.Is(err, ErrInvalidName) {
		t.Fatal(err)
	}
	if _, err := l.Read("x", 4, 0, 1); !errors.Is(err, ErrInvalidPartition) {
		t.Fatal(err)
	}
	if _, err := l.Read("x", 0, 0, 0); !errors.Is(err, ErrInvalidLimit) {
		t.Fatal(err)
	}
	r, err := l.Read("missing", 0, 0, 1)
	if err != nil || len(r.Records) != 0 || r.Generation != 0 {
		t.Fatalf("missing=%+v %v", r, err)
	}
	_, _, _ = l.AppendBatch([]Input{{Topic: "z"}, {Topic: "a"}})
	s := l.Snapshot()
	if len(s.Topics) != 2 || s.Topics[0].Topic != "a" || s.Topics[1].Topic != "z" {
		t.Fatalf("topics=%+v", s.Topics)
	}
}
