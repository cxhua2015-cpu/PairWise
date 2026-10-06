package metacatalog216

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
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func (s *Store) Apply(b Batch) (Result, error) {
	// Full structural validation before touching any state.
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

	if len(b.Ops) == 0 {
		return Result{Generation: s.generation, Revision: s.revision}, nil
	}

	// Candidate transaction: clone state, apply ops in input order.
	cand := make(map[string]entry, len(s.records))
	for k, v := range s.records {
		cand[k] = v
	}
	rev := s.revision
	touched := make(map[string]struct{})
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			rev++
			v := make([]byte, len(op.Value))
			copy(v, op.Value)
			cand[op.Name] = entry{value: v, revision: rev}
			touched[op.Name] = struct{}{}
		case Delete:
			if _, ok := cand[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
			touched[op.Name] = struct{}{}
		}
	}

	// Capacity limits are checked only against the final batch state.
	if len(cand) > s.opts.MaxRecords {
		return Result{}, ErrCapacity
	}
	total := 0
	for _, e := range cand {
		total += len(e.value)
	}
	if total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	// Commit.
	s.records = cand
	s.revision = rev
	s.generation++

	names := make([]string, 0, len(touched))
	for name := range touched {
		if _, ok := cand[name]; ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	changed := make([]Record, 0, len(names))
	for _, name := range names {
		e := cand[name]
		v := make([]byte, len(e.value))
		copy(v, e.value)
		changed = append(changed, Record{Name: name, Value: v, Revision: e.revision})
	}
	return Result{Generation: s.generation, Revision: s.revision, Changed: changed}, nil
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
