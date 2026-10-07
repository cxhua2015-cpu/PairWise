package metacatalog391

import (
	"errors"
	"sort"
	"sync"
)

var (
	ErrNotImplemented = errors.New("not implemented")
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrNotFound       = errors.New("not found")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Kind uint8

const (
	Put Kind = iota + 1
	Delete
)

type Options struct{ MaxRecords, MaxNameBytes, MaxValueBytes, MaxTotalValueBytes int }
type Op struct {
	Kind  Kind
	Name  string
	Value []byte
}
type Batch struct{ Ops []Op }
type Record struct {
	Name     string
	Value    []byte
	Revision uint64
}
type Result struct {
	Generation, Revision uint64
	Changed              []Record
}
type Snapshot struct {
	Generation, NextRevision uint64
	Records                  []Record
}

type entry struct {
	value    []byte
	revision uint64
}

type Store struct {
	mu         sync.RWMutex
	opts       Options
	records    map[string]entry
	totalBytes int
	generation uint64
	revision   uint64
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: o, records: make(map[string]entry)}, nil
}

func validName(name string, maxBytes int) bool {
	if name == "" || len(name) > maxBytes {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

func (s *Store) Apply(b Batch) (Result, error) {
	// 阶段一：完整结构校验，不读取任何状态。
	for _, op := range b.Ops {
		if op.Kind != Put && op.Kind != Delete {
			return Result{}, ErrInvalidInput
		}
		if !validName(op.Name, s.opts.MaxNameBytes) {
			return Result{}, ErrInvalidInput
		}
		if op.Kind == Put && len(op.Value) > s.opts.MaxValueBytes {
			return Result{}, ErrInvalidInput
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// 阶段二：在候选状态上按输入顺序执行。
	type candEntry struct {
		value    []byte
		revision uint64
		exists   bool
	}
	cand := make(map[string]candEntry, len(b.Ops))
	touched := make(map[string]bool, len(b.Ops))
	candRecords := len(s.records)
	candBytes := s.totalBytes
	revision := s.revision

	lookup := func(name string) (candEntry, bool) {
		if e, ok := cand[name]; ok {
			return e, e.exists
		}
		if e, ok := s.records[name]; ok {
			return candEntry{value: e.value, revision: e.revision, exists: true}, true
		}
		return candEntry{}, false
	}

	for _, op := range b.Ops {
		touched[op.Name] = true
		switch op.Kind {
		case Put:
			old, existed := lookup(op.Name)
			if existed {
				candBytes -= len(old.value)
			} else {
				candRecords++
			}
			revision++
			v := make([]byte, len(op.Value))
			copy(v, op.Value)
			cand[op.Name] = candEntry{value: v, revision: revision, exists: true}
			candBytes += len(v)
		case Delete:
			old, existed := lookup(op.Name)
			if !existed {
				return Result{}, ErrNotFound
			}
			cand[op.Name] = candEntry{exists: false}
			candRecords--
			candBytes -= len(old.value)
		}
	}

	// 阶段三：批次末容量检查。
	if candRecords > s.opts.MaxRecords || candBytes > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	// 阶段四：提交。
	for name, e := range cand {
		if e.exists {
			s.records[name] = entry{value: e.value, revision: e.revision}
		} else {
			delete(s.records, name)
		}
	}
	s.totalBytes = candBytes
	s.revision = revision
	if len(b.Ops) > 0 {
		s.generation++
	}

	names := make([]string, 0, len(touched))
	for name := range touched {
		if _, ok := s.records[name]; ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	changed := make([]Record, 0, len(names))
	for _, name := range names {
		e := s.records[name]
		v := make([]byte, len(e.value))
		copy(v, e.value)
		changed = append(changed, Record{Name: name, Value: v, Revision: e.revision})
	}
	return Result{Generation: s.generation, Revision: revision, Changed: changed}, nil
}

func (s *Store) Get(name string) (Record, bool, error) {
	if !validName(name, s.opts.MaxNameBytes) {
		return Record{}, false, ErrInvalidInput
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.records[name]
	if !ok {
		return Record{}, false, nil
	}
	v := make([]byte, len(e.value))
	copy(v, e.value)
	return Record{Name: name, Value: v, Revision: e.revision}, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.records))
	for name := range s.records {
		names = append(names, name)
	}
	sort.Strings(names)
	recs := make([]Record, 0, len(names))
	for _, name := range names {
		e := s.records[name]
		v := make([]byte, len(e.value))
		copy(v, e.value)
		recs = append(recs, Record{Name: name, Value: v, Revision: e.revision})
	}
	return Snapshot{Generation: s.generation, NextRevision: s.revision + 1, Records: recs}
}
