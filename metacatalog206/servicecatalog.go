package metacatalog206

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
	mu         sync.RWMutex
	opts       Options
	records    map[string]Record
	totalValue int
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
	if len(name) == 0 || len(name) > maxBytes {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

func (s *Store) validate(b Batch) error {
	for _, op := range b.Ops {
		if !validName(op.Name, s.opts.MaxNameBytes) {
			return ErrInvalidInput
		}
		switch op.Kind {
		case Put:
			if len(op.Value) > s.opts.MaxValueBytes {
				return ErrInvalidInput
			}
		case Delete:
			if len(op.Value) != 0 {
				return ErrInvalidInput
			}
		default:
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
	if len(b.Ops) == 0 {
		return Result{Generation: s.generation, Revision: s.revision}, nil
	}

	// Candidate transaction: apply on a copy so failure rolls back everything.
	records := make(map[string]Record, len(s.records))
	for k, v := range s.records {
		records[k] = v
	}
	totalValue := s.totalValue
	revision := s.revision
	touched := make(map[string]struct{})

	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			old, existed := records[op.Name]
			if existed {
				totalValue -= len(old.Value)
			}
			revision++
			v := make([]byte, len(op.Value))
			copy(v, op.Value)
			records[op.Name] = Record{Name: op.Name, Value: v, Revision: revision}
			totalValue += len(v)
			touched[op.Name] = struct{}{}
		case Delete:
			old, existed := records[op.Name]
			if !existed {
				return Result{}, ErrNotFound
			}
			totalValue -= len(old.Value)
			delete(records, op.Name)
			touched[op.Name] = struct{}{}
		}
	}

	if len(records) > s.opts.MaxRecords || totalValue > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	s.records = records
	s.totalValue = totalValue
	s.revision = revision
	s.generation++

	changed := make([]Record, 0, len(touched))
	for name := range touched {
		if r, ok := records[name]; ok {
			changed = append(changed, cloneRecord(r))
		}
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].Name < changed[j].Name })

	return Result{Generation: s.generation, Revision: s.revision, Changed: changed}, nil
}

func (s *Store) Get(name string) (Record, bool, error) {
	if !validName(name, s.opts.MaxNameBytes) {
		return Record{}, false, ErrInvalidInput
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.records[name]
	if !ok {
		return Record{}, false, nil
	}
	return cloneRecord(r), true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	records := make([]Record, 0, len(s.records))
	for _, r := range s.records {
		records = append(records, cloneRecord(r))
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Name < records[j].Name })
	return Snapshot{Generation: s.generation, NextRevision: s.revision + 1, Records: records}
}

func cloneRecord(r Record) Record {
	v := make([]byte, len(r.Value))
	copy(v, r.Value)
	return Record{Name: r.Name, Value: v, Revision: r.Revision}
}
