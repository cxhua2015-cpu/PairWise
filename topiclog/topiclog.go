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

const (
	maxPartitions   = 64
	maxTopics       = 10000
	maxRecordsLimit = 1_000_000
	maxPayloadLimit = 64 << 20
	maxNameLen      = 64
	maxKeyLen       = 256
	maxPayloadLen   = 1 << 20
	maxReadLimit    = 1000
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

type partition struct {
	high    uint64
	records []Record // strictly ascending offsets, records[i].Offset == base+1+i
	base    uint64
}

type topic struct{ parts []partition }

type commitKey struct {
	group, topic string
	partition    int
}

type Log struct {
	mu          sync.RWMutex
	opts        Options
	gen         uint64
	topics      map[string]*topic
	commits     map[commitKey]uint64
	usedRecords int
	usedPayload int
}

func New(o Options) (*Log, error) {
	if o.Partitions < 1 || o.Partitions > maxPartitions ||
		o.MaxTopics < 1 || o.MaxTopics > maxTopics ||
		o.MaxRecords < 1 || o.MaxRecords > maxRecordsLimit ||
		o.MaxPayloadBytes < 1 || o.MaxPayloadBytes > maxPayloadLimit {
		return nil, fmt.Errorf("topiclog: New: %w", ErrInvalidOptions)
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
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func partitionOf(key string, partitions int) int {
	if key == "" {
		return 0
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return int(h.Sum32() % uint32(partitions))
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
	for i, in := range inputs {
		if !validName(in.Topic) {
			return nil, 0, fmt.Errorf("topiclog: append input %d: %w", i, ErrInvalidName)
		}
		if len(in.Key) > maxKeyLen {
			return nil, 0, fmt.Errorf("topiclog: append input %d: %w", i, ErrInvalidKey)
		}
		if len(in.Payload) > maxPayloadLen {
			return nil, 0, fmt.Errorf("topiclog: append input %d: %w", i, ErrPayloadTooLarge)
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(inputs) == 0 {
		return nil, l.gen, nil
	}

	out := make([]Record, 0, len(inputs))
	var newTopics []string
	appended := 0
	rollback := func() {
		for i := appended - 1; i >= 0; i-- {
			r := out[i]
			p := &l.topics[r.Topic].parts[r.Partition]
			p.records = p.records[:len(p.records)-1]
			p.high--
			l.usedRecords--
			l.usedPayload -= len(r.Payload)
		}
		for _, name := range newTopics {
			delete(l.topics, name)
		}
	}

	for _, in := range inputs {
		t, ok := l.topics[in.Topic]
		if !ok {
			t = &topic{parts: make([]partition, l.opts.Partitions)}
			l.topics[in.Topic] = t
			newTopics = append(newTopics, in.Topic)
		}
		pi := partitionOf(in.Key, l.opts.Partitions)
		p := &t.parts[pi]
		stored := clonePayload(in.Payload)
		p.high++
		p.records = append(p.records, Record{
			Topic: in.Topic, Partition: pi, Offset: p.high, Key: in.Key, Payload: stored,
		})
		l.usedRecords++
		l.usedPayload += len(stored)
		appended++
		out = append(out, Record{
			Topic: in.Topic, Partition: pi, Offset: p.high, Key: in.Key, Payload: clonePayload(in.Payload),
		})
	}

	if len(l.topics) > l.opts.MaxTopics || l.usedRecords > l.opts.MaxRecords || l.usedPayload > l.opts.MaxPayloadBytes {
		rollback()
		return nil, 0, fmt.Errorf("topiclog: append: %w", ErrCapacity)
	}
	l.gen++
	return out, l.gen, nil
}

func (l *Log) Read(topicName string, partition int, after uint64, limit int) (ReadResult, error) {
	if !validName(topicName) {
		return ReadResult{}, fmt.Errorf("topiclog: read: %w", ErrInvalidName)
	}
	if partition < 0 || partition >= l.opts.Partitions {
		return ReadResult{}, fmt.Errorf("topiclog: read: %w", ErrInvalidPartition)
	}
	if limit < 1 || limit > maxReadLimit {
		return ReadResult{}, fmt.Errorf("topiclog: read: %w", ErrInvalidLimit)
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	res := ReadResult{Generation: l.gen}
	t, ok := l.topics[topicName]
	if !ok {
		return res, nil
	}
	p := &t.parts[partition]
	start := sort.Search(len(p.records), func(i int) bool { return p.records[i].Offset > after })
	for i := start; i < len(p.records) && len(res.Records) < limit; i++ {
		res.Records = append(res.Records, cloneRecord(p.records[i]))
	}
	return res, nil
}

func (l *Log) CommitBatch(commits []Commit) (uint64, error) {
	for i, c := range commits {
		if !validName(c.Group) || !validName(c.Topic) {
			return 0, fmt.Errorf("topiclog: commit %d: %w", i, ErrInvalidName)
		}
		if c.Partition < 0 || c.Partition >= l.opts.Partitions {
			return 0, fmt.Errorf("topiclog: commit %d: %w", i, ErrInvalidPartition)
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(commits) == 0 {
		return l.gen, nil
	}

	type undoEntry struct {
		key     commitKey
		old     uint64
		existed bool
	}
	var undos []undoEntry
	rollback := func() {
		for i := len(undos) - 1; i >= 0; i-- {
			u := undos[i]
			if u.existed {
				l.commits[u.key] = u.old
			} else {
				delete(l.commits, u.key)
			}
		}
	}

	for i, c := range commits {
		key := commitKey{c.Group, c.Topic, c.Partition}
		old, existed := l.commits[key]
		if !existed {
			old = 0
		}
		if c.Offset < old {
			rollback()
			return 0, fmt.Errorf("topiclog: commit %d: %w", i, ErrOffsetRegression)
		}
		var high uint64
		if t, ok := l.topics[c.Topic]; ok {
			high = t.parts[c.Partition].high
		}
		if c.Offset > high {
			rollback()
			return 0, fmt.Errorf("topiclog: commit %d: %w", i, ErrOffsetAhead)
		}
		undos = append(undos, undoEntry{key, old, existed})
		l.commits[key] = c.Offset
	}
	l.gen++
	return l.gen, nil
}

func (l *Log) Trim(topicName string, partition int, through uint64) (uint64, error) {
	if !validName(topicName) {
		return 0, fmt.Errorf("topiclog: trim: %w", ErrInvalidName)
	}
	if partition < 0 || partition >= l.opts.Partitions {
		return 0, fmt.Errorf("topiclog: trim: %w", ErrInvalidPartition)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	t, ok := l.topics[topicName]
	if !ok {
		return 0, fmt.Errorf("topiclog: trim: %w", ErrNotFound)
	}
	known := false
	for k, off := range l.commits {
		if k.topic == topicName && k.partition == partition {
			known = true
			if off < through {
				return 0, fmt.Errorf("topiclog: trim: %w", ErrUnsafeTrim)
			}
		}
	}
	if !known {
		return 0, fmt.Errorf("topiclog: trim: %w", ErrUnsafeTrim)
	}
	p := &t.parts[partition]
	n := sort.Search(len(p.records), func(i int) bool { return p.records[i].Offset > through })
	for _, r := range p.records[:n] {
		l.usedRecords--
		l.usedPayload -= len(r.Payload)
	}
	if n > 0 {
		rest := make([]Record, len(p.records)-n)
		copy(rest, p.records[n:])
		p.records = rest
		p.base += uint64(n)
	}
	l.gen++
	return l.gen, nil
}

func (l *Log) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	s := Snapshot{
		Generation:       l.gen,
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
		for i := range t.parts {
			p := &t.parts[i]
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
		s.Commits = append(s.Commits, Commit{Group: k.group, Topic: k.topic, Partition: k.partition, Offset: l.commits[k]})
	}
	return s
}
