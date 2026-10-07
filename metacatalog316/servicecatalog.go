package metacatalog316

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
	mu          sync.RWMutex
	opts        Options
	records     map[string]Record
	totalValue  int
	generation  uint64
	nextRevision uint64
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: o, records: make(map[string]Record), nextRevision: 1}, nil
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

	// Candidate transaction: apply ops on a copy, commit only on success.
	cand := make(map[string]Record, len(s.records))
	for k, v := range s.records {
		cand[k] = v
	}
	total := s.totalValue
	nextRev := s.nextRevision
	changed := make(map[string]Record)

	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			var cp []byte
			if op.Value != nil {
				cp = make([]byte, len(op.Value))
				copy(cp, op.Value)
			}
			old, existed := cand[op.Name]
			if existed {
				total -= len(old.Value)
			}
			rec := Record{Name: op.Name, Value: cp, Revision: nextRev}
			nextRev++
			cand[op.Name] = rec
			total += len(cp)
			changed[op.Name] = rec
		case Delete:
			old, existed := cand[op.Name]
			if !existed {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
			total -= len(old.Value)
			delete(changed, op.Name)
		}
	}

	if len(cand) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	s.records = cand
	s.totalValue = total
	s.nextRevision = nextRev
	if len(b.Ops) > 0 {
		s.generation++
	}

	res := Result{Generation: s.generation, Revision: nextRev - 1}
	if len(changed) > 0 {
		names := make([]string, 0, len(changed))
		for n := range changed {
			names = append(names, n)
		}
		sort.Strings(names)
		res.Changed = make([]Record, 0, len(names))
		for _, n := range names {
			res.Changed = append(res.Changed, changed[n])
		}
	}
	return res, nil
}

func (s *Store) Get(name string) (Record, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !validName(name, s.opts.MaxNameBytes) {
		return Record{}, false, ErrInvalidInput
	}
	rec, ok := s.records[name]
	if !ok {
		return Record{}, false, nil
	}
	if rec.Value != nil {
		cp := make([]byte, len(rec.Value))
		copy(cp, rec.Value)
		rec.Value = cp
	}
	return rec, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap := Snapshot{Generation: s.generation, NextRevision: s.nextRevision}
	if len(s.records) > 0 {
		names := make([]string, 0, len(s.records))
		for n := range s.records {
			names = append(names, n)
		}
		sort.Strings(names)
		snap.Records = make([]Record, 0, len(names))
		for _, n := range names {
			rec := s.records[n]
			if rec.Value != nil {
				cp := make([]byte, len(rec.Value))
				copy(cp, rec.Value)
				rec.Value = cp
			}
			snap.Records = append(snap.Records, rec)
		}
	}
	return snap
}
