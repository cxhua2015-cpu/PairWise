package metacatalog346

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

func validName(name string, maxBytes int) bool {
	if len(name) == 0 || len(name) > maxBytes {
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
	if len(b.Ops) == 0 {
		return Result{Generation: s.generation}, nil
	}

	// Candidate transaction: work on copies so failure leaves no trace.
	records := make(map[string]entry, len(s.records))
	for k, v := range s.records {
		records[k] = v
	}
	totalValue := s.totalValue
	nextRevision := s.nextRevision
	touched := make(map[string]struct{})

	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			if old, ok := records[op.Name]; ok {
				totalValue -= len(old.value)
			}
			value := make([]byte, len(op.Value))
			copy(value, op.Value)
			records[op.Name] = entry{value: value, revision: nextRevision}
			totalValue += len(value)
			nextRevision++
			touched[op.Name] = struct{}{}
		case Delete:
			old, ok := records[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			totalValue -= len(old.value)
			delete(records, op.Name)
			touched[op.Name] = struct{}{}
		}
	}

	if len(records) > s.opts.MaxRecords || totalValue > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	s.records = records
	s.totalValue = totalValue
	s.nextRevision = nextRevision
	s.generation++

	changed := make([]Record, 0, len(touched))
	for name := range touched {
		if e, ok := records[name]; ok {
			changed = append(changed, Record{Name: name, Value: append([]byte(nil), e.value...), Revision: e.revision})
		}
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].Name < changed[j].Name })

	return Result{Generation: s.generation, Revision: nextRevision - 1, Changed: changed}, nil
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
	return Record{Name: name, Value: append([]byte(nil), e.value...), Revision: e.revision}, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	records := make([]Record, 0, len(s.records))
	for name, e := range s.records {
		records = append(records, Record{Name: name, Value: append([]byte(nil), e.value...), Revision: e.revision})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Name < records[j].Name })
	return Snapshot{Generation: s.generation, NextRevision: s.nextRevision, Records: records}
}
