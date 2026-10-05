package imagecatalog

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
	mu                sync.RWMutex
	maxRecords        int
	maxNameBytes      int
	maxValueBytes     int
	maxTotalValueByte int
	records           map[string]Record
	totalValueBytes   int
	generation        uint64
	nextRevision      uint64
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{
		maxRecords:        o.MaxRecords,
		maxNameBytes:      o.MaxNameBytes,
		maxValueBytes:     o.MaxValueBytes,
		maxTotalValueByte: o.MaxTotalValueBytes,
		records:           make(map[string]Record),
		nextRevision:      1,
	}, nil
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

func cloneRecord(r Record) Record {
	r.Value = append([]byte(nil), r.Value...)
	return r
}

func (s *Store) validateOp(op Op) error {
	switch op.Kind {
	case Put:
		if !validName(op.Name, s.maxNameBytes) || len(op.Value) > s.maxValueBytes {
			return ErrInvalidInput
		}
	case Delete:
		if !validName(op.Name, s.maxNameBytes) || len(op.Value) != 0 {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

func (s *Store) Apply(b Batch) (Result, error) {
	for _, op := range b.Ops {
		if err := s.validateOp(op); err != nil {
			return Result{}, err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	cand := make(map[string]Record, len(s.records))
	for name, rec := range s.records {
		cand[name] = rec
	}
	total := s.totalValueBytes
	next := s.nextRevision
	touched := make(map[string]struct{}, len(b.Ops))

	for _, op := range b.Ops {
		touched[op.Name] = struct{}{}
		switch op.Kind {
		case Put:
			if old, ok := cand[op.Name]; ok {
				total -= len(old.Value)
			}
			value := append([]byte(nil), op.Value...)
			cand[op.Name] = Record{Name: op.Name, Value: value, Revision: next}
			next++
			total += len(value)
		case Delete:
			old, ok := cand[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			total -= len(old.Value)
			delete(cand, op.Name)
		}
	}

	if len(cand) > s.maxRecords || total > s.maxTotalValueByte {
		return Result{}, ErrCapacity
	}

	if len(b.Ops) > 0 {
		s.generation++
	}
	s.records = cand
	s.totalValueBytes = total
	s.nextRevision = next

	changed := make([]Record, 0, len(touched))
	for name := range touched {
		if rec, ok := cand[name]; ok {
			changed = append(changed, cloneRecord(rec))
		}
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].Name < changed[j].Name })

	return Result{Generation: s.generation, Revision: s.nextRevision - 1, Changed: changed}, nil
}

func (s *Store) Get(name string) (Record, bool, error) {
	if !validName(name, s.maxNameBytes) {
		return Record{}, false, ErrInvalidInput
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.records[name]
	if !ok {
		return Record{}, false, nil
	}
	return cloneRecord(rec), true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	records := make([]Record, 0, len(s.records))
	for _, rec := range s.records {
		records = append(records, cloneRecord(rec))
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Name < records[j].Name })
	return Snapshot{Generation: s.generation, NextRevision: s.nextRevision, Records: records}
}
