package resourcecatalog106

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

type Store struct {
	mu         sync.Mutex
	opts       Options
	records    map[string]Record
	generation uint64
	revision   uint64
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: o, records: make(map[string]Record)}, nil
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

func (s *Store) validate(b Batch) error {
	for _, op := range b.Ops {
		if op.Kind != Put && op.Kind != Delete {
			return ErrInvalidInput
		}
		if !validName(op.Name, s.opts.MaxNameBytes) {
			return ErrInvalidInput
		}
		if op.Kind == Put && len(op.Value) > s.opts.MaxValueBytes {
			return ErrInvalidInput
		}
	}
	return nil
}

func (s *Store) Apply(b Batch) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.validate(b); err != nil {
		return Result{}, err
	}

	candidate := make(map[string]Record, len(s.records)+len(b.Ops))
	for k, v := range s.records {
		candidate[k] = v
	}
	revision := s.revision
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			revision++
			v := make([]byte, len(op.Value))
			copy(v, op.Value)
			candidate[op.Name] = Record{Name: op.Name, Value: v, Revision: revision}
		case Delete:
			if _, ok := candidate[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
		}
	}

	if len(candidate) > s.opts.MaxRecords {
		return Result{}, ErrCapacity
	}
	total := 0
	for _, r := range candidate {
		total += len(r.Value)
	}
	if total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	s.records = candidate
	s.revision = revision
	if len(b.Ops) > 0 {
		s.generation++
	}

	touched := make(map[string]struct{}, len(b.Ops))
	for _, op := range b.Ops {
		touched[op.Name] = struct{}{}
	}
	names := make([]string, 0, len(touched))
	for name := range touched {
		names = append(names, name)
	}
	sort.Strings(names)
	changed := make([]Record, 0, len(names))
	for _, name := range names {
		if r, ok := candidate[name]; ok {
			changed = append(changed, Record{Name: r.Name, Value: append([]byte(nil), r.Value...), Revision: r.Revision})
		}
	}
	return Result{Generation: s.generation, Revision: s.revision, Changed: changed}, nil
}

func (s *Store) Get(name string) (Record, bool, error) {
	if !validName(name, s.opts.MaxNameBytes) {
		return Record{}, false, ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[name]
	if !ok {
		return Record{}, false, nil
	}
	return Record{Name: r.Name, Value: append([]byte(nil), r.Value...), Revision: r.Revision}, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, len(s.records))
	for name := range s.records {
		names = append(names, name)
	}
	sort.Strings(names)
	records := make([]Record, 0, len(names))
	for _, name := range names {
		r := s.records[name]
		records = append(records, Record{Name: r.Name, Value: append([]byte(nil), r.Value...), Revision: r.Revision})
	}
	return Snapshot{Generation: s.generation, NextRevision: s.revision + 1, Records: records}
}
