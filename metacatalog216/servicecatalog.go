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
	mu           sync.RWMutex
	opts         Options
	records      map[string]entry
	totalValue   int
	generation   uint64
	nextRevision uint64
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: o, records: make(map[string]entry), nextRevision: 1}, nil
}

func validName(s *Store, name string) bool {
	if len(name) == 0 || len(name) > s.opts.MaxNameBytes {
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
	s.mu.Lock()
	defer s.mu.Unlock()

	// Phase 1: full structural validation before any state read.
	for _, op := range b.Ops {
		if op.Kind != Put && op.Kind != Delete {
			return Result{}, ErrInvalidInput
		}
		if !validName(s, op.Name) {
			return Result{}, ErrInvalidInput
		}
		if op.Kind == Put && len(op.Value) > s.opts.MaxValueBytes {
			return Result{}, ErrInvalidInput
		}
	}

	if len(b.Ops) == 0 {
		return Result{Generation: s.generation, Revision: s.nextRevision - 1}, nil
	}

	// Phase 2: apply against a candidate view (copy-on-write), reading state.
	candidate := make(map[string]entry, len(s.records))
	for k, v := range s.records {
		candidate[k] = v
	}
	total := s.totalValue
	revision := s.nextRevision
	touched := make(map[string]struct{})

	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			if old, ok := candidate[op.Name]; ok {
				total -= len(old.value)
			}
			vc := make([]byte, len(op.Value))
			copy(vc, op.Value)
			candidate[op.Name] = entry{value: vc, revision: revision}
			total += len(vc)
			revision++
		case Delete:
			old, ok := candidate[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			total -= len(old.value)
			delete(candidate, op.Name)
		}
		touched[op.Name] = struct{}{}
	}

	// Phase 3: capacity checks only at batch end.
	if len(candidate) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	// Commit.
	s.records = candidate
	s.totalValue = total
	s.nextRevision = revision
	s.generation++

	changed := make([]Record, 0, len(touched))
	for name := range touched {
		if e, ok := candidate[name]; ok {
			changed = append(changed, Record{Name: name, Value: append([]byte(nil), e.value...), Revision: e.revision})
		}
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].Name < changed[j].Name })

	return Result{Generation: s.generation, Revision: revision - 1, Changed: changed}, nil
}

func (s *Store) Get(name string) (Record, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !validName(s, name) {
		return Record{}, false, ErrInvalidInput
	}
	e, ok := s.records[name]
	if !ok {
		return Record{}, false, nil
	}
	return Record{Name: name, Value: append([]byte(nil), e.value...), Revision: e.revision}, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	recs := make([]Record, 0, len(s.records))
	for name, e := range s.records {
		recs = append(recs, Record{Name: name, Value: append([]byte(nil), e.value...), Revision: e.revision})
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].Name < recs[j].Name })
	return Snapshot{Generation: s.generation, NextRevision: s.nextRevision, Records: recs}
}
