package metacatalog411

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
	totalBytes int
	generation uint64
	nextRev    uint64
}

func New(opts Options) (*Store, error) {
	if opts.MaxRecords <= 0 || opts.MaxNameBytes <= 0 || opts.MaxValueBytes <= 0 || opts.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: opts, records: make(map[string]Record), nextRev: 1}, nil
}

func (s *Store) Apply(b Batch) (Result, error) {
	if err := s.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	candidate := make(map[string]Record, len(s.records)+len(b.Ops))
	for name, rec := range s.records {
		candidate[name] = rec
	}
	total := s.totalBytes
	nextRev := s.nextRev
	changed := make(map[string]Record)
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			value := make([]byte, len(op.Value))
			copy(value, op.Value)
			if old, ok := candidate[op.Name]; ok {
				total -= len(old.Value)
			}
			rec := Record{Name: op.Name, Value: value, Revision: nextRev}
			nextRev++
			candidate[op.Name] = rec
			total += len(value)
			changed[op.Name] = rec
		case Delete:
			old, ok := candidate[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
			total -= len(old.Value)
			delete(changed, op.Name)
		}
	}
	if len(candidate) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	s.records = candidate
	s.totalBytes = total
	s.nextRev = nextRev
	res := Result{Generation: s.generation, Revision: nextRev - 1}
	if len(b.Ops) > 0 {
		s.generation++
		res.Generation = s.generation
	}
	if len(changed) > 0 {
		ordered := make([]Record, 0, len(changed))
		for _, c := range changed {
			ordered = append(ordered, c)
		}
		sort.Slice(ordered, func(i, j int) bool { return ordered[i].Name < ordered[j].Name })
		res.Changed = make([]Record, len(ordered))
		for i, c := range ordered {
			value := make([]byte, len(c.Value))
			copy(value, c.Value)
			c.Value = value
			res.Changed[i] = c
		}
	}
	return res, nil
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
	value := make([]byte, len(rec.Value))
	copy(value, rec.Value)
	rec.Value = value
	return rec, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap := Snapshot{Generation: s.generation, NextRevision: s.nextRev}
	if len(s.records) > 0 {
		snap.Records = make([]Record, 0, len(s.records))
		for _, rec := range s.records {
			value := make([]byte, len(rec.Value))
			copy(value, rec.Value)
			rec.Value = value
			snap.Records = append(snap.Records, rec)
		}
		sort.Slice(snap.Records, func(i, j int) bool { return snap.Records[i].Name < snap.Records[j].Name })
	}
	return snap
}
