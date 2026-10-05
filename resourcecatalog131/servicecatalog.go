package resourcecatalog131

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
	mu         sync.Mutex
	opts       Options
	records    map[string]entry
	totalBytes int
	generation uint64
	revision   uint64
}

func New(opts Options) (*Store, error) {
	if opts.MaxRecords <= 0 || opts.MaxNameBytes <= 0 || opts.MaxValueBytes <= 0 || opts.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: opts, records: make(map[string]entry)}, nil
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

	candidate := make(map[string]entry, len(s.records)+len(b.Ops))
	for k, v := range s.records {
		candidate[k] = v
	}
	total := s.totalBytes
	revision := s.revision
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			revision++
			if old, ok := candidate[op.Name]; ok {
				total -= len(old.value)
			}
			v := make([]byte, len(op.Value))
			copy(v, op.Value)
			candidate[op.Name] = entry{value: v, revision: revision}
			total += len(v)
		case Delete:
			old, ok := candidate[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
			total -= len(old.value)
		}
	}
	if len(candidate) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	if len(b.Ops) > 0 {
		s.generation++
	}
	s.records = candidate
	s.totalBytes = total
	s.revision = revision

	seen := make(map[string]bool, len(b.Ops))
	final := make([]Record, 0, len(b.Ops))
	for _, op := range b.Ops {
		if seen[op.Name] {
			continue
		}
		seen[op.Name] = true
		if e, ok := s.records[op.Name]; ok {
			v := make([]byte, len(e.value))
			copy(v, e.value)
			final = append(final, Record{Name: op.Name, Value: v, Revision: e.revision})
		}
	}
	sort.Slice(final, func(i, j int) bool { return final[i].Name < final[j].Name })
	return Result{Generation: s.generation, Revision: revision, Changed: final}, nil
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
	recs := make([]Record, 0, len(s.records))
	for name, e := range s.records {
		v := make([]byte, len(e.value))
		copy(v, e.value)
		recs = append(recs, Record{Name: name, Value: v, Revision: e.revision})
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].Name < recs[j].Name })
	return Snapshot{Generation: s.generation, NextRevision: s.revision + 1, Records: recs}
}
