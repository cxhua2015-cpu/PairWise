package metacatalog311

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

func (s *Store) Apply(b Batch) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Phase 1: full structural validation before reading any state.
	for _, op := range b.Ops {
		if op.Kind != Put && op.Kind != Delete {
			return Result{}, ErrInvalidInput
		}
		if !validName(op.Name, s.opts.MaxNameBytes) {
			return Result{}, ErrInvalidInput
		}
		switch op.Kind {
		case Put:
			if len(op.Value) > s.opts.MaxValueBytes {
				return Result{}, ErrInvalidInput
			}
		case Delete:
			if len(op.Value) != 0 {
				return Result{}, ErrInvalidInput
			}
		}
	}

	if len(b.Ops) == 0 {
		return Result{Generation: s.generation, Revision: s.revision}, nil
	}

	// Phase 2: execute against a candidate copy; commit only on success.
	candidate := make(map[string]Record, len(s.records))
	for k, v := range s.records {
		candidate[k] = v
	}
	revision := s.revision
	touched := make(map[string]struct{})
	for _, op := range b.Ops {
		touched[op.Name] = struct{}{}
		switch op.Kind {
		case Put:
			revision++
			value := make([]byte, len(op.Value))
			copy(value, op.Value)
			candidate[op.Name] = Record{Name: op.Name, Value: value, Revision: revision}
		case Delete:
			if _, ok := candidate[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
		}
	}

	// Final capacity checks happen only at the end of the batch.
	total := 0
	for _, r := range candidate {
		total += len(r.Value)
	}
	if len(candidate) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	s.records = candidate
	s.revision = revision
	s.generation++

	changed := make([]Record, 0, len(touched))
	for name := range touched {
		if r, ok := candidate[name]; ok {
			changed = append(changed, cloneRecord(r))
		}
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].Name < changed[j].Name })
	return Result{Generation: s.generation, Revision: revision, Changed: changed}, nil
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
	return cloneRecord(r), true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	records := make([]Record, 0, len(s.records))
	for _, r := range s.records {
		records = append(records, cloneRecord(r))
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Name < records[j].Name })
	return Snapshot{Generation: s.generation, NextRevision: s.revision + 1, Records: records}
}

func cloneRecord(r Record) Record {
	value := make([]byte, len(r.Value))
	copy(value, r.Value)
	return Record{Name: r.Name, Value: value, Revision: r.Revision}
}
