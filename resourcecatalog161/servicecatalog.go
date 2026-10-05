package resourcecatalog161

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
	mu      sync.RWMutex
	opts    Options
	records map[string]Record
	total   int
	gen     uint64
	nextRev uint64
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: o, records: make(map[string]Record), nextRev: 1}, nil
}

func validName(s string, max int) bool {
	if len(s) == 0 || len(s) > max {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
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
		if len(op.Value) > s.opts.MaxValueBytes {
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
	// Candidate transaction on a copy; committed only on success.
	cand := make(map[string]Record, len(s.records))
	for k, v := range s.records {
		cand[k] = v
	}
	total := s.total
	nextRev := s.nextRev
	changed := make(map[string]Record)
	deleted := make(map[string]bool)
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			r := Record{Name: op.Name, Revision: nextRev}
			if op.Value != nil {
				r.Value = append([]byte(nil), op.Value...)
			}
			if old, ok := cand[op.Name]; ok {
				total -= len(old.Value)
			}
			total += len(r.Value)
			cand[op.Name] = r
			changed[op.Name] = r
			deleted[op.Name] = false
			nextRev++
		case Delete:
			old, ok := cand[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			total -= len(old.Value)
			delete(cand, op.Name)
			delete(changed, op.Name)
			deleted[op.Name] = true
		}
	}
	if len(cand) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}
	if len(b.Ops) > 0 {
		s.gen++
	}
	s.records = cand
	s.total = total
	s.nextRev = nextRev
	res := Result{Generation: s.gen, Revision: nextRev - 1}
	names := make([]string, 0, len(changed))
	for n := range changed {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		res.Changed = append(res.Changed, changed[n])
	}
	return res, nil
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
	if r.Value != nil {
		r.Value = append([]byte(nil), r.Value...)
	}
	return r, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap := Snapshot{Generation: s.gen, NextRevision: s.nextRev}
	names := make([]string, 0, len(s.records))
	for n := range s.records {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		r := s.records[n]
		if r.Value != nil {
			r.Value = append([]byte(nil), r.Value...)
		}
		snap.Records = append(snap.Records, r)
	}
	return snap
}
