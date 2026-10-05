package resourcecatalog181

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
	mu           sync.Mutex
	opts         Options
	records      map[string]Record
	generation   uint64
	nextRevision uint64
	totalValue   int
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: o, records: make(map[string]Record), nextRevision: 1}, nil
}

func validName(name string, max int) bool {
	if len(name) == 0 || len(name) > max {
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

func (s *Store) validateOp(op Op) error {
	switch op.Kind {
	case Put:
		if !validName(op.Name, s.opts.MaxNameBytes) || len(op.Value) > s.opts.MaxValueBytes {
			return ErrInvalidInput
		}
	case Delete:
		if !validName(op.Name, s.opts.MaxNameBytes) || len(op.Value) != 0 {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

func (s *Store) Apply(b Batch) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Phase 1: full structural validation before any state read.
	for _, op := range b.Ops {
		if err := s.validateOp(op); err != nil {
			return Result{}, err
		}
	}

	// Phase 2: candidate transaction on a cloned index.
	records := make(map[string]Record, len(s.records))
	for k, v := range s.records {
		records[k] = v
	}
	total := s.totalValue
	nextRev := s.nextRevision
	touched := make(map[string]struct{})

	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			value := make([]byte, len(op.Value))
			copy(value, op.Value)
			if old, ok := records[op.Name]; ok {
				total -= len(old.Value)
			}
			records[op.Name] = Record{Name: op.Name, Value: value, Revision: nextRev}
			total += len(value)
			nextRev++
			touched[op.Name] = struct{}{}
		case Delete:
			old, ok := records[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(records, op.Name)
			total -= len(old.Value)
			touched[op.Name] = struct{}{}
		}
	}

	// Phase 3: capacity limits checked only at batch end.
	if len(records) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	// Commit.
	s.records = records
	s.totalValue = total
	s.nextRevision = nextRev
	if len(b.Ops) > 0 {
		s.generation++
	}

	changed := make([]Record, 0, len(touched))
	for name := range touched {
		if rec, ok := records[name]; ok {
			changed = append(changed, cloneRecord(rec))
		}
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].Name < changed[j].Name })

	return Result{Generation: s.generation, Revision: nextRev - 1, Changed: changed}, nil
}

func cloneRecord(r Record) Record {
	v := make([]byte, len(r.Value))
	copy(v, r.Value)
	return Record{Name: r.Name, Value: v, Revision: r.Revision}
}

func (s *Store) Get(name string) (Record, bool, error) {
	if !validName(name, s.opts.MaxNameBytes) {
		return Record{}, false, ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[name]
	if !ok {
		return Record{}, false, nil
	}
	return cloneRecord(rec), true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	records := make([]Record, 0, len(s.records))
	for _, rec := range s.records {
		records = append(records, cloneRecord(rec))
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Name < records[j].Name })
	return Snapshot{Generation: s.generation, NextRevision: s.nextRevision, Records: records}
}
