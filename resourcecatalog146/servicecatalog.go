package resourcecatalog146

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
	mu           sync.Mutex
	opts         Options
	records      map[string]entry
	generation   uint64
	nextRevision uint64
}

func New(opts Options) (*Store, error) {
	if opts.MaxRecords <= 0 || opts.MaxNameBytes <= 0 || opts.MaxValueBytes <= 0 || opts.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: opts, records: make(map[string]entry), nextRevision: 1}, nil
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
	// Full structural validation before any state read.
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
		if op.Kind == Delete && len(op.Value) != 0 {
			return Result{}, ErrInvalidInput
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Candidate transaction: clone current state, apply ops in input order.
	candidate := make(map[string]entry, len(s.records))
	for k, v := range s.records {
		candidate[k] = v
	}
	nextRev := s.nextRevision
	touched := make(map[string]bool)
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			v := make([]byte, len(op.Value))
			copy(v, op.Value)
			candidate[op.Name] = entry{value: v, revision: nextRev}
			nextRev++
			touched[op.Name] = true
		case Delete:
			if _, ok := candidate[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
			touched[op.Name] = true
		}
	}

	// Capacity checks only at batch end.
	if len(candidate) > s.opts.MaxRecords {
		return Result{}, ErrCapacity
	}
	total := 0
	for _, e := range candidate {
		total += len(e.value)
	}
	if total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	// Commit.
	s.records = candidate
	s.nextRevision = nextRev
	if len(b.Ops) > 0 {
		s.generation++
	}
	changed := make([]string, 0, len(touched))
	for name := range touched {
		if _, ok := candidate[name]; ok {
			changed = append(changed, name)
		}
	}
	sort.Strings(changed)
	res := Result{Generation: s.generation, Revision: nextRev - 1}
	for _, name := range changed {
		e := candidate[name]
		v := make([]byte, len(e.value))
		copy(v, e.value)
		res.Changed = append(res.Changed, Record{Name: name, Value: v, Revision: e.revision})
	}
	return res, nil
}

func (s *Store) Get(name string) (Record, bool, error) {
	if !validName(name, s.opts.MaxNameBytes) {
		return Record{}, false, ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.records[name]
	if !ok {
		return Record{}, false, nil
	}
	v := make([]byte, len(e.value))
	copy(v, e.value)
	return Record{Name: name, Value: v, Revision: e.revision}, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, len(s.records))
	for name := range s.records {
		names = append(names, name)
	}
	sort.Strings(names)
	snap := Snapshot{Generation: s.generation, NextRevision: s.nextRevision, Records: make([]Record, 0, len(names))}
	for _, name := range names {
		e := s.records[name]
		v := make([]byte, len(e.value))
		copy(v, e.value)
		snap.Records = append(snap.Records, Record{Name: name, Value: v, Revision: e.revision})
	}
	return snap
}
