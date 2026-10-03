package topiclog

import (
	"errors"
	"fmt"
	"hash/fnv"
	"sort"
	"sync"
)

var (
	ErrNotImplemented   = errors.New("topiclog: not implemented")
	ErrInvalidOptions   = errors.New("topiclog: invalid options")
	ErrInvalidName      = errors.New("topiclog: invalid name")
	ErrInvalidKey       = errors.New("topiclog: invalid key")
	ErrPayloadTooLarge  = errors.New("topiclog: payload too large")
	ErrInvalidPartition = errors.New("topiclog: invalid partition")
	ErrInvalidLimit     = errors.New("topiclog: invalid limit")
	ErrCapacity         = errors.New("topiclog: capacity exceeded")
	ErrOffsetRegression = errors.New("topiclog: offset regression")
	ErrOffsetAhead      = errors.New("topiclog: offset ahead")
	ErrNotFound         = errors.New("topiclog: not found")
	ErrUnsafeTrim       = errors.New("topiclog: unsafe trim")
)

type Options struct{ Partitions, MaxTopics, MaxRecords, MaxPayloadBytes int }
type Input struct {
	Topic, Key string
	Payload    []byte
}
type Record struct {
	Topic     string
	Partition int
	Offset    uint64
	Key       string
	Payload   []byte
}
type Commit struct {
	Group, Topic string
	Partition    int
	Offset       uint64
}
type ReadResult struct {
	Generation uint64
	Records    []Record
}
type PartitionView struct {
	Partition  int
	HighOffset uint64
	Records    []Record
}
type TopicView struct {
	Topic      string
	Partitions []PartitionView
}
type Snapshot struct {
	Generation                    uint64
	UsedRecords, UsedPayloadBytes int
	Topics                        []TopicView
	Commits                       []Commit
}

const (
	maxPartitions = 64
	maxTopics     = 10000
	maxRecords    = 1000000
	maxPayload    = 64 << 20
	maxNameLen    = 64
	maxKeyLen     = 256
	maxRecordSize = 1 << 20
	maxReadLimit  = 1000
)

type partition struct {
	records []Record // sorted by offset, contiguous from base+1
	base    uint64   // offset of records[0] minus 1 (trimmed high water)
	high    uint64   // highest offset ever assigned
}

type topic struct {
	parts []partition
}

type commitKey struct {
	group, topic string
	partition    int
}

// Log is a concurrency-safe in-memory partitioned log.
type Log struct {
	mu          sync.Mutex
	opts        Options
	topics      map[string]*topic
	commits     map[commitKey]uint64
	generation  uint64
	usedRecords int
	usedPayload int
}

func New(o Options) (*Log, error) {
	if o.Partitions < 1 || o.Partitions > maxPartitions ||
		o.MaxTopics < 1 || o.MaxTopics > maxTopics ||
		o.MaxRecords < 1 || o.MaxRecords > maxRecords ||
		o.MaxPayloadBytes < 1 || o.MaxPayloadBytes > maxPayload {
		return nil, fmt.Errorf("%w: %+v", ErrInvalidOptions, o)
	}
	return &Log{
		opts:    o,
		topics:  make(map[string]*topic),
		commits: make(map[commitKey]uint64),
	}, nil
}

func validName(s string) bool {
	if len(s) < 1 || len(s) > maxNameLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '.' || c == '_' || c == '-' {
			continue
		}
		return false
	}
	return true
}

func partitionOf(key string, n int) int {
	if key == "" {
		return 0
	}
	h := fnv.New32a()
	h.Write([]byte(key))
	return int(h.Sum32() % uint32(n))
}

func clonePayload(p []byte) []byte {
	if p == nil {
		return nil
	}
	c := make([]byte, len(p))
	copy(c, p)
	return c
}

func cloneRecord(r Record) Record {
	r.Payload = clonePayload(r.Payload)
	return r
}

func (l *Log) AppendBatch(inputs []Input) ([]Record, uint64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(inputs) == 0 {
		return nil, l.generation, nil
	}
	// Structural validation of the whole batch first.
	for _, in := range inputs {
		if !validName(in.Topic) {
			return nil, 0, fmt.Errorf("%w: topic %q", ErrInvalidName, in.Topic)
		}
		if len(in.Key) > maxKeyLen {
			return nil, 0, fmt.Errorf("%w: key of %d bytes", ErrInvalidKey, len(in.Key))
		}
		if len(in.Payload) > maxRecordSize {
			return nil, 0, fmt.Errorf("%w: %d bytes", ErrPayloadTooLarge, len(in.Payload))
		}
	}
	// Apply on a candidate copy; roll back by discarding it.
	cand := make(map[string]*topic, len(l.topics))
	for name, t := range l.topics {
		nt := &topic{parts: make([]partition, len(t.parts))}
		for i, p := range t.parts {
			nt.parts[i] = partition{records: p.records, base: p.base, high: p.high}
		}
		cand[name] = nt
	}
	usedRecords, usedPayload := l.usedRecords, l.usedPayload
	out := make([]Record, 0, len(inputs))
	for _, in := range inputs {
		t, ok := cand[in.Topic]
		if !ok {
			t = &topic{parts: make([]partition, l.opts.Partitions)}
			cand[in.Topic] = t
		}
		p := partitionOf(in.Key, l.opts.Partitions)
		part := &t.parts[p]
		part.high++
		rec := Record{
			Topic:     in.Topic,
			Partition: p,
			Offset:    part.high,
			Key:       in.Key,
			Payload:   clonePayload(in.Payload),
		}
		part.records = append(part.records, rec)
		usedRecords++
		usedPayload += len(in.Payload)
		out = append(out, cloneRecord(rec))
	}
	// Capacity checks on the final candidate state only.
	if len(cand) > l.opts.MaxTopics || usedRecords > l.opts.MaxRecords || usedPayload > l.opts.MaxPayloadBytes {
		return nil, 0, fmt.Errorf("%w: topics=%d records=%d payload=%d",
			ErrCapacity, len(cand), usedRecords, usedPayload)
	}
	// Commit candidate: share record slices (immutable after commit).
	l.topics = cand
	l.usedRecords, l.usedPayload = usedRecords, usedPayload
	l.generation++
	return out, l.generation, nil
}

func (l *Log) Read(topicName string, partition int, after uint64, limit int) (ReadResult, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !validName(topicName) {
		return ReadResult{}, fmt.Errorf("%w: topic %q", ErrInvalidName, topicName)
	}
	if partition < 0 || partition >= l.opts.Partitions {
		return ReadResult{}, fmt.Errorf("%w: %d", ErrInvalidPartition, partition)
	}
	if limit < 1 || limit > maxReadLimit {
		return ReadResult{}, fmt.Errorf("%w: %d", ErrInvalidLimit, limit)
	}
	res := ReadResult{Generation: l.generation}
	t, ok := l.topics[topicName]
	if !ok {
		return res, nil
	}
	p := &t.parts[partition]
	// records are contiguous from base+1; skip those with Offset <= after.
	start := 0
	if after > p.base {
		start = int(after - p.base)
		if start > len(p.records) {
			start = len(p.records)
		}
	}
	for i := start; i < len(p.records) && len(res.Records) < limit; i++ {
		res.Records = append(res.Records, cloneRecord(p.records[i]))
	}
	return res, nil
}

func (l *Log) CommitBatch(commits []Commit) (uint64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(commits) == 0 {
		return l.generation, nil
	}
	for _, c := range commits {
		if !validName(c.Group) || !validName(c.Topic) {
			return 0, fmt.Errorf("%w: group %q topic %q", ErrInvalidName, c.Group, c.Topic)
		}
		if c.Partition < 0 || c.Partition >= l.opts.Partitions {
			return 0, fmt.Errorf("%w: %d", ErrInvalidPartition, c.Partition)
		}
	}
	cand := make(map[commitKey]uint64, len(l.commits)+len(commits))
	for k, v := range l.commits {
		cand[k] = v
	}
	for _, c := range commits {
		k := commitKey{c.Group, c.Topic, c.Partition}
		if c.Offset < cand[k] {
			return 0, fmt.Errorf("%w: group %q topic %q partition %d: %d < %d",
				ErrOffsetRegression, c.Group, c.Topic, c.Partition, c.Offset, cand[k])
		}
		var high uint64
		if t, ok := l.topics[c.Topic]; ok {
			high = t.parts[c.Partition].high
		}
		if c.Offset > high {
			return 0, fmt.Errorf("%w: group %q topic %q partition %d: %d > %d",
				ErrOffsetAhead, c.Group, c.Topic, c.Partition, c.Offset, high)
		}
		cand[k] = c.Offset
	}
	l.commits = cand
	l.generation++
	return l.generation, nil
}

func (l *Log) Trim(topicName string, partition int, through uint64) (uint64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !validName(topicName) {
		return 0, fmt.Errorf("%w: topic %q", ErrInvalidName, topicName)
	}
	if partition < 0 || partition >= l.opts.Partitions {
		return 0, fmt.Errorf("%w: %d", ErrInvalidPartition, partition)
	}
	t, ok := l.topics[topicName]
	if !ok {
		return 0, fmt.Errorf("%w: topic %q", ErrNotFound, topicName)
	}
	known := false
	for k, off := range l.commits {
		if k.topic == topicName && k.partition == partition {
			known = true
			if off < through {
				return 0, fmt.Errorf("%w: group %q at %d < %d",
					ErrUnsafeTrim, k.group, off, through)
			}
		}
	}
	if !known {
		return 0, fmt.Errorf("%w: no known consumer group for %q partition %d",
			ErrUnsafeTrim, topicName, partition)
	}
	p := &t.parts[partition]
	drop := 0
	if through > p.base {
		drop = int(through - p.base)
		if drop > len(p.records) {
			drop = len(p.records)
		}
	}
	if drop > 0 {
		for _, r := range p.records[:drop] {
			l.usedPayload -= len(r.Payload)
		}
		kept := make([]Record, len(p.records)-drop)
		copy(kept, p.records[drop:])
		p.records = kept
		p.base += uint64(drop)
		l.usedRecords -= drop
	}
	l.generation++
	return l.generation, nil
}

func (l *Log) Snapshot() Snapshot {
	l.mu.Lock()
	defer l.mu.Unlock()

	s := Snapshot{
		Generation:       l.generation,
		UsedRecords:      l.usedRecords,
		UsedPayloadBytes: l.usedPayload,
	}
	names := make([]string, 0, len(l.topics))
	for name := range l.topics {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		t := l.topics[name]
		tv := TopicView{Topic: name, Partitions: make([]PartitionView, len(t.parts))}
		for i, p := range t.parts {
			pv := PartitionView{Partition: i, HighOffset: p.high}
			for _, r := range p.records {
				pv.Records = append(pv.Records, cloneRecord(r))
			}
			tv.Partitions[i] = pv
		}
		s.Topics = append(s.Topics, tv)
	}
	keys := make([]commitKey, 0, len(l.commits))
	for k := range l.commits {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.group != b.group {
			return a.group < b.group
		}
		if a.topic != b.topic {
			return a.topic < b.topic
		}
		return a.partition < b.partition
	})
	for _, k := range keys {
		s.Commits = append(s.Commits, Commit{
			Group: k.group, Topic: k.topic, Partition: k.partition, Offset: l.commits[k],
		})
	}
	return s
}
