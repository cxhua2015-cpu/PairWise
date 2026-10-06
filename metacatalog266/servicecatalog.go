package metacatalog266

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
	generation uint64
	nextRev    uint64
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: o, records: make(map[string]Record), nextRev: 1}, nil
}

func (s *Store) Apply(b Batch) (Result, error) {
	if err := s.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	candidate := make(map[string]Record, len(s.records))
	for name, rec := range s.records {
		candidate[name] = rec
	}
	nextRev := s.nextRev
	touched := make(map[string]struct{}, len(b.Ops))
	for _, op := range b.Ops {
		touched[op.Name] = struct{}{}
		switch op.Kind {
		case Put:
			value := make([]byte, len(op.Value))
			copy(value, op.Value)
			candidate[op.Name] = Record{Name: op.Name, Value: value, Revision: nextRev}
			nextRev++
		case Delete:
			if _, ok := candidate[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
		}
	}
	total := 0
	for _, rec := range candidate {
		total += len(rec.Value)
	}
	if len(candidate) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	changed := make([]Record, 0, len(touched))
	for name := range touched {
		if rec, ok := candidate[name]; ok {
			changed = append(changed, cloneRecord(rec))
		}
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].Name < changed[j].Name })

	s.records = candidate
	s.nextRev = nextRev
	if len(b.Ops) > 0 {
		s.generation++
	}
	return Result{Generation: s.generation, Revision: nextRev - 1, Changed: changed}, nil
}

func (s *Store) Get(name string) (Record, bool, error) {
	if err := validateName(name, s.opts.MaxNameBytes); err != nil {
		return Record{}, false, err
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
	return Snapshot{Generation: s.generation, NextRevision: s.nextRev, Records: records}
}

func cloneRecord(r Record) Record {
	value := make([]byte, len(r.Value))
	copy(value, r.Value)
	return Record{Name: r.Name, Value: value, Revision: r.Revision}
}
