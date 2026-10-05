package resourcecatalog116

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

type record struct {
	value    []byte
	revision uint64
}

type Store struct {
	mu           sync.Mutex
	opts         Options
	records      map[string]record
	totalValue   int
	generation   uint64
	nextRevision uint64
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: o, records: make(map[string]record), nextRevision: 1}, nil
}

func validName(name string, maxBytes int) bool {
	if len(name) == 0 || len(name) > maxBytes {
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

func (s *Store) validate(op Op) error {
	if op.Kind != Put && op.Kind != Delete {
		return ErrInvalidInput
	}
	if !validName(op.Name, s.opts.MaxNameBytes) {
		return ErrInvalidInput
	}
	if op.Kind == Put && len(op.Value) > s.opts.MaxValueBytes {
		return ErrInvalidInput
	}
	return nil
}

func (s *Store) Apply(b Batch) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Full structural validation before any state is read.
	for _, op := range b.Ops {
		if err := s.validate(op); err != nil {
			return Result{}, err
		}
	}

	// Candidate transaction: apply ops in input order on a copy.
	cand := make(map[string]record, len(s.records)+len(b.Ops))
	total := s.totalValue
	for name, rec := range s.records {
		cand[name] = rec
	}
	changed := make(map[string]Record)
	var rev uint64
	nextRev := s.nextRevision
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			if old, ok := cand[op.Name]; ok {
				total -= len(old.value)
			}
			cp := make([]byte, len(op.Value))
			copy(cp, op.Value)
			cand[op.Name] = record{value: cp, revision: nextRev}
			changed[op.Name] = Record{Name: op.Name, Value: cp, Revision: nextRev}
			rev = nextRev
			nextRev++
			total += len(cp)
		case Delete:
			old, ok := cand[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
			changed[op.Name] = Record{Name: op.Name}
			total -= len(old.value)
		}
	}

	// Capacity limits are checked only at the end of the batch.
	if len(cand) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	s.records = cand
	s.totalValue = total
	s.nextRevision = nextRev
	if len(b.Ops) > 0 {
		s.generation++
	}

	names := make([]string, 0, len(changed))
	for name := range changed {
		names = append(names, name)
	}
	sort.Strings(names)
	res := Result{Generation: s.generation, Revision: rev, Changed: make([]Record, 0, len(names))}
	for _, name := range names {
		rec := changed[name]
		if rec.Value != nil {
			cp := make([]byte, len(rec.Value))
			copy(cp, rec.Value)
			rec.Value = cp
		}
		res.Changed = append(res.Changed, rec)
	}
	return res, nil
}

func (s *Store) Get(name string) (Record, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validName(name, s.opts.MaxNameBytes) {
		return Record{}, false, ErrInvalidInput
	}
	rec, ok := s.records[name]
	if !ok {
		return Record{}, false, nil
	}
	cp := make([]byte, len(rec.value))
	copy(cp, rec.value)
	return Record{Name: name, Value: cp, Revision: rec.revision}, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, len(s.records))
	for name := range s.records {
		names = append(names, name)
	}
	sort.Strings(names)
	snap := Snapshot{Generation: s.generation, NextRevision: s.nextRevision, Records: make([]Record, 0, len(names))}
	for _, name := range names {
		rec := s.records[name]
		cp := make([]byte, len(rec.value))
		copy(cp, rec.value)
		snap.Records = append(snap.Records, Record{Name: name, Value: cp, Revision: rec.revision})
	}
	return snap
}
